---

# Product Requirements Document (PRD): `chrome-ctl`

**Document Version:** 1.0

**Status:** Draft / Ready for Implementation

**Target Environment:** Local CLI + Chrome Extension (Manifest V3)

**Primary Consumer:** Autonomous AI Agents (e.g., Hermes Agent, Claude Code, OpenCode)

---

## 1. Executive Summary & Vision

Traditional browser automation tools (Playwright, Puppeteer, Selenium) spin up isolated, easily fingerprinted browser instances and often flood LLM context windows with raw HTML/DOM trees, burning thousands of tokens per page load.

**`chrome-ctl`** solves both problems:

1. **Zero-Detection Session Reuse:** By operating via a lightweight Chrome Extension installed inside the user's primary, authenticated Google Chrome browser, the agent inherits active cookies, Google SSO sessions, and genuine browser fingerprints.
2. **Extreme Token Efficiency:** Instead of dumping raw DOM or verbose Accessibility (AX) trees, the extension assigns short numeric IDs (`[1]`, `[2]`, `[3]`) only to visible, interactive elements and returns a compact, flat text map. Visual outputs (screenshots and recordings) are aggressively downscaled, compressed to WebP/WebM, and frame-decimated before touching the agent's context window.
3. **Self-Learning Macro Engine:** Repeated high-value workflows (e.g., directory registrations, LinkedIn outreach sequences) can be recorded and parameterized into deterministic, zero-token replay playbooks.

---

## 2. System Architecture

```
+-------------------------------------------------------------------------+
|                           AI Agent (Hermes)                             |
|       Reads compact stdout (<500 tokens/turn) & executes CLI cmds       |
+------------------------------------+------------------------------------+
                                     |
                                     v  (Subprocess / Bash execution)
+-------------------------------------------------------------------------+
|                        `chrome-ctl` CLI Binary                          |
|   - Stateless command client & local daemon (Go / Node / Python)        |
|   - Media Optimizer (Sharp/FFmpeg: WebP, Set-of-Mark, 2fps WebM)        |
|   - Playbook / Macro Storage (`~/.chrome-ctl/macros/*.json`)            |
+------------------------------------+------------------------------------+
                                     |
                                     v  (Local WebSocket: ws://127.0.0.1:9229)
+-------------------------------------------------------------------------+
|                    Chrome Extension (Manifest V3)                       |
|   - Background Service Worker (Tab routing, `chrome.debugger` attach)   |
|   - Content Script (Semantic DOM distillation, Bounding Box overlays)   |
|   - Humanizer Engine (Bezier mouse curves, cadence typing, scroll jitter)|
+------------------------------------+------------------------------------+
                                     |
                                     v
+-------------------------------------------------------------------------+
|                    User's Active Google Chrome                          |
|   Instagram | LinkedIn | Facebook | Google OAuth SSO | Directory Sites  |
+-------------------------------------------------------------------------+

```

---

## 3. Core Functional Requirements

### 3.1 Token-Minimal DOM Distillation (`snap`)

To prevent context-window bloat, the CLI **never** outputs raw HTML unless explicitly queried via CSS selector.

* **Interactive Indexing:** The Content Script scans the current viewport (or full page) for interactive nodes (`a`, `button`, `input`, `textarea`, `select`, `[role="button"]`, `[contenteditable="true"]`).
* **Compact Format:** Outputs a single line per actionable item using a strict token-saving schema:
`[ID] <TAG:type> "Label/Text" (value/state) @x,y`
* **Viewport Filtering:** By default, `chrome-ctl snap` only outputs elements currently in or near the viewport (`--all` flag overrides this), keeping standard snapshots between **150 and 450 tokens**.

### 3.2 Vision & Recording Token Optimization (`shot` & `rec`)

When the DOM is obfuscated (e.g., canvas elements, complex React portals on Instagram/Facebook) or when the agent needs to verify task completion visually:

* **Smart Screenshots (`chrome-ctl shot`):**
* Captures the active tab via `chrome.tabs.captureVisibleTab`.
* **Set-of-Mark (SoM) Overlay (`--som`):** Draws high-contrast numeric badges (`[1]`, `[2]`) directly over interactive elements on the image so Vision LLMs can reference element IDs without guessing coordinates.
* **Compression Pipeline:** Automatically downscales max width to `1024px` (configurable), converts to **WebP (Quality 65)** or **Grayscale WebP (`--gray`)** for text-heavy verification. Target file size: **25 KB – 60 KB**.


* **Optimized Video Recording (`chrome-ctl rec`):**
* Captures tab media stream or sequential frames during task execution.
* **Token-Aware Encoding:** Encodes to **WebM (VP9)** at **1 to 2 FPS**, `720p` max resolution, stripping audio tracks.
* **Contact Sheet Mode (`--grid`):** Can stitch a 15-second task recording into a single `3x4` timestamped WebP storyboard image—allowing an LLM to "watch" an entire workflow verification using the vision tokens of a **single image**.



### 3.3 Human-Like Behavior & Anti-Bot Evasion

Social platforms (LinkedIn, Instagram, Facebook) monitor behavioral biometrics. All interaction commands pass through the **Humanizer Engine** by default:

* **Trusted Hardware Events:** Uses `chrome.debugger` API (`Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`) so all events carry `event.isTrusted = true`.
* **Bezier Mouse Trajectories:** Moving to an element generates a cubic Bezier curve with micro-jitter and variable velocity (Fitts's Law acceleration/deceleration) before clicking.
* **Randomized Click Offsets:** Clicks land within a Gaussian distribution around the center of the target bounding box (never dead-center `(0,0)` repeatedly).
* **Cadence Typing:** Keystrokes fire with variable delays (`45ms–140ms`), faster bursts for common bigrams (`th`, `in`, `er`), pause delays at punctuation, and an optional `--typo-rate 0.02` that types an adjacent key and immediately hits `Backspace`.
* **Natural Reading Scrolls:** `chrome-ctl scroll` uses momentum wheel deltas with short reading pauses rather than instant `window.scrollTo` jumps.

### 3.4 Google SSO & Authenticated Session Handling

* Because the extension runs inside the user's primary Chrome profile, active sessions are preserved.
* **OAuth Popup Interceptor (`chrome-ctl sso`):** When clicking "Continue with Google" or "Sign up with Google" on third-party sites (Clutch, DesignRush, SaaS directories, etc.), the Background Worker automatically detects the newly spawned `accounts.google.com` popup window or redirect, selects the specified user email account, clicks "Continue/Allow", and returns control to the parent tab seamlessly.

### 3.5 Self-Learning Workflow & Macro Engine (`macro`)

When an agent completes an important, repeatable task (or records one), it can save the execution trace as a parameterized macro.

* **Action Logging:** Every CLI command executed during a session is logged with the target element's underlying resilient selectors (ARIA label, text match, relative XPath, CSS path).
* **Parameterization:** Hardcoded inputs (e.g., company name, message body, target profile URL) are converted into variables (`{{company_name}}`, `{{pitch_text}}`).
* **Zero-Token Replay:** Running `chrome-ctl macro run <name> --vars '...'` executes the entire multi-step sequence locally via the CLI daemon without calling the LLM between steps—consuming **0 LLM tokens** unless an unexpected selector change triggers a fallback alert.

---

## 4. CLI Command Specification

| Category | Command Syntax | Description |
| --- | --- | --- |
| **Session & Tab** | `chrome-ctl status` | Check daemon & Chrome extension connection status. |
|  | `chrome-ctl url` | Print the active tab's URL (cheap "where am I" call). |
|  | `chrome-ctl tabs [--window <id>]` | List open tabs with `[TabID] Title - URL`. |
|  | `chrome-ctl open <url> [--new] [--window <id>]` | Navigate current tab or open a new tab. |
|  | `chrome-ctl switch <tab_id>` | Switch active control to specified tab. |
| **Background Window** | `chrome-ctl window ensure` | Reuse or create an unfocused automation window; never steals focus. |
|  | `chrome-ctl window list` | Show every window, which is focused (`*`), which is `(automation)`. |
|  | `chrome-ctl window new [url] [--foreground]` | Force a fresh automation window. |
|  | `chrome-ctl window use <id\|focused>` / `clear` | Point automation at another window, or stop targeting one. |
|  | `chrome-ctl window close <id> [--force]` | Close a window; refuses the focused (user) window without `--force`. |
| **Observation (Text)** | `chrome-ctl snap [--all] [--forms] [--deep] [--max-elements N]` | Output ultra-compact indexed interactive element map. |
|  | `chrome-ctl read [<id_or_selector>] [--tables\|--links] [--json]` | Extract markdown, structured tables, or link lists. |
|  | `chrome-ctl describe <id_or_selector>` | Full attribute record for one element (role, state, href, rect, match count). |
|  | `chrome-ctl href <id>` | Print the element's absolute, resolved href. |
| **Observation (Visual)** | `chrome-ctl shot [--som] [--gray] [-o path]` | Capture token-optimized WebP screenshot (optional Set-of-Mark IDs). |
|  | `chrome-ctl rec start [--fps 2]` | Start low-bitrate 2fps WebM or storyboard recording. |
|  | `chrome-ctl rec stop [--grid] [-o path]` | Stop recording; outputs WebM file or a single stitched WebP grid. |
| **Diagnostics** | `chrome-ctl net [--errors] [--since 5m] [--limit 50] [--json]` | Show recent CDP network activity for the tab. |
| **Human Actions** | `chrome-ctl click <id>` | Humanized Bezier mouse move + trusted click on element `[id]`. |
|  | `chrome-ctl clickat <x>,<y> [--double]` | Exact viewport CSS-pixel click for controls `snap` cannot see. |
|  | `chrome-ctl type <id> "<text>" [--enter]` | Click element `[id]`, type with human cadence, verify the read-back. |
|  | `chrome-ctl press <key>` | Send trusted key press (`Enter`, `Tab`, `Escape`, `ArrowDown`). |
|  | `chrome-ctl scroll <down\|up> [amount]` | Smooth momentum scroll (`amount` in viewports, default `0.8`). |
|  | `chrome-ctl upload <id> <file_path>` | Attach local file (e.g., company logo, post image) to file input `[id]`. |
| **Escape Hatch** | `chrome-ctl eval '<js>' [--raw] [--timeout 5000]` | Evaluate JS in the page via CDP; disabled with daemon `--no-eval`. |
| **Auth & Helpers** | `chrome-ctl sso [--account <email>]` | Auto-complete active Google OAuth popup/redirect flow. |
|  | `chrome-ctl wait <ms\|selector\|idle>` | Wait for a pause, element appearance, or DOM idle. |
|  | `chrome-ctl wait --network [--quiet-ms 500]` | Wait until CDP-observed network activity goes quiet. |
|  | `chrome-ctl wait --for-text "..." [--ignore-case]` | Wait until text appears anywhere in the page. |
| **Bulk** | `chrome-ctl bulk <file\|-> --out <dir> [--template '...{{item}}']` | Visit many URLs/queries, write pages to files, emit a manifest. |
| **Macro / Reuse** | `chrome-ctl macro start <name>` | Begin recording CLI actions into a reusable playbook. |
|  | `chrome-ctl macro save [--params k1,k2]` | Save recorded steps to `~/.chrome-ctl/macros/<name>.json`. |
|  | `chrome-ctl macro list` | List all saved reusable workflows and required parameters. |
|  | `chrome-ctl macro run <name> --vars '<json>'` | Replay saved workflow locally with zero LLM token overhead. |

Snapshot IDs are unique for the life of a document: `snap` never renumbers an existing element, so an old ID either resolves to the same element or reports `not found`. A navigation or reload invalidates every ID and is reported as `snapshot stale` (`-32010`). `snap` prints `SNAP <n> |` so the epoch is visible. Action success means the input was dispatched, not that the site accepted it; `type` reports `(verified)` or a `WARN` when the read-back is empty or mismatched.

---

## 5. Output Wire Format Specification (Token Budget Comparison)

**Standard Playwright/HTML Dump:** ~8,500 tokens

**Standard Accessibility Tree:** ~2,200 tokens

**`chrome-ctl snap` Output:** **~95 tokens**

```text
TAB [4] LinkedIn - Messaging | https://www.linkedin.com/messaging/
VIEWPORT: 0-900px of 2400px (37%)
[1] <input:search> "Search messages" () @240,115
[2] <button> "Compose message" @380,115
[3] <div:listitem> "Rahul Verma - Founder at TechCraft" @240,210
[4] <div:textbox> "Write a message…" () @680,740
[5] <button> "Attach an image or file" @520,790
[6] <button:submit> "Send" (disabled) @860,790

```

Every action command (`click`, `type`, `scroll`) returns a **1-line delta response** (~15 tokens) rather than re-dumping the page, unless `--snap` is appended:

```text
OK: Typed 142 chars into [4] (human cadence: 4.8s). State changed: [6] <button:submit> "Send" is now (enabled).

```

---

## 6. Agent Skill Definition (`SKILL.md`)

The agent-facing skill ships at the repository root: [`SKILL.md`](../SKILL.md). This section intentionally does not duplicate it — an embedded copy drifted from the real file and taught outdated command syntax.

Install it into your agent's skills directory (e.g. `~/.claude/skills/chrome-ctl/SKILL.md` or `~/.hermes/skills/chrome-ctl/SKILL.md`) by copying that file as-is. It is the authoritative guide for:

* The observe → act → verify loop, including `url`, `describe`, and `SNAP <n> |` snapshot epochs.
* Snapshot identities: globally unique IDs, navigation resets, and stale-ID recovery.
* Reaching controls `snap` cannot see (`--deep`, `describe`, keyboard fallback, `clickat`, `eval`).
* SPA waiting (`wait --network`, `wait --for-text`) versus DOM-only `wait idle`.
* Structural extraction (`read --tables`, `read --links`), network history (`net`), and `bulk` runs.
* Honest failure semantics: dispatched-but-unconfirmed timeouts, `WARN` read-backs, and capture fallbacks.
* Web pitfalls agents routinely hit (guessed deep links, consent banners, controlled inputs, infinite scroll, rate limits, downloads) and where to stop and hand back to the user.

---

## 7. Implementation Roadmap (Suggested Tech Stack)

To keep the tool fast, zero-cost, and local-first on macOS/Linux:
1. **CLI & Local Daemon (`Go` or `Bun/TypeScript`):**
   * **Go** compiles to a single instant-startup binary (`chrome-ctl`) with zero runtime overhead, using `gorilla/websocket` for the extension bridge and `libwebp` / `ffmpeg` bindings for image/video compression.
2. **Chrome Extension (Manifest V3, Vanilla TS):**
   * `permissions`: `["activeTab", "tabs", "debugger", "scripting", "storage"]`
   * `host_permissions`: `["<all_urls>"]`
   * Keeps a persistent WebSocket connection (`ws://127.0.0.1:9229`) alive via MV3 offscreen document or periodic heartbeat alarm.


