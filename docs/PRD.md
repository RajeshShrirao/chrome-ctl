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
|  | `chrome-ctl tabs [--window <id>]` | List open tabs with `[TabID] Title - URL`. |
|  | `chrome-ctl open <url> [--new] [--window <id>]` | Navigate current tab or open a new tab. |
|  | `chrome-ctl switch <tab_id>` | Switch active control to specified tab. |
| **Background Window** | `chrome-ctl window ensure` | Reuse or create an unfocused automation window; never steals focus. |
|  | `chrome-ctl window list` | Show every window, which is focused (`*`), which is `(automation)`. |
|  | `chrome-ctl window new [url] [--foreground]` | Force a fresh automation window. |
|  | `chrome-ctl window use <id\|focused>` / `clear` | Point automation at another window, or stop targeting one. |
|  | `chrome-ctl window close <id> [--force]` | Close a window; refuses the focused (user) window without `--force`. |
| **Observation (Text)** | `chrome-ctl snap [--all] [--forms]` | Output ultra-compact indexed interactive element map. |
|  | `chrome-ctl read [<id_or_selector>]` | Extract clean markdown text from page or specific container. |
| **Observation (Visual)** | `chrome-ctl shot [--som] [--gray] [-o path]` | Capture token-optimized WebP screenshot (optional Set-of-Mark IDs). |
|  | `chrome-ctl rec start [--fps 2]` | Start low-bitrate 2fps WebM or storyboard recording. |
|  | `chrome-ctl rec stop [--grid] [-o path]` | Stop recording; outputs WebM file or a single stitched WebP grid. |
| **Human Actions** | `chrome-ctl click <id>` | Humanized Bezier mouse move + trusted click on element `[id]`. |
|  | `chrome-ctl type <id> "<text>" [--enter]` | Click element `[id]` and type with human cadence + optional Enter. |
|  | `chrome-ctl press <key>` | Send trusted key press (`Enter`, `Tab`, `Escape`, `ArrowDown`). |
|  | `chrome-ctl scroll <down|up> [amount]` | Smooth momentum scroll (`amount` in viewports, default `0.8`). |
|  | `chrome-ctl upload <id> <file_path>` | Attach local file (e.g., company logo, post image) to file input `[id]`. |
| **Auth & Helpers** | `chrome-ctl sso [--account <email>]` | Auto-complete active Google OAuth popup/redirect flow. |
|  | `chrome-ctl wait <ms|selector|idle>` | Wait for human pause, element appearance, or network idle. |
| **Macro / Reuse** | `chrome-ctl macro start <name>` | Begin recording CLI actions into a reusable playbook. |
|  | `chrome-ctl macro save [--params k1,k2]` | Save recorded steps to `~/.chrome-ctl/macros/<name>.json`. |
|  | `chrome-ctl macro list` | List all saved reusable workflows and required parameters. |
|  | `chrome-ctl macro run <name> --vars '<json>'` | Replay saved workflow locally with zero LLM token overhead. |

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

Save the following file directly into your agent's skills directory (e.g., `~/.hermes/skills/chrome-ctl/SKILL.md` or `.claude/skills/chrome-ctl/SKILL.md`).

```markdown
---
name: chrome-ctl
description: Control the user's authenticated Google Chrome browser with ultra-low token overhead, human-like anti-detection behavior, Google OAuth SSO handling, optimized visual snapshots/recordings, and self-learning workflow macros. Use for social media automation (LinkedIn, Instagram, Facebook), business directory registrations, lead outreach, and personal assistant browsing tasks.
---

# Chrome Control (`chrome-ctl`) Agent Skill

You have access to `chrome-ctl`, a high-efficiency CLI bridge connected to the user's real, logged-in Google Chrome browser. 

## Core Principles for Token & Stealth Efficiency

1. **NEVER request raw HTML.** Always use `chrome-ctl snap` to get a compact list of interactive element IDs (`[1]`, `[2]`, etc.), or `chrome-ctl snap --forms` when filling out registrations.
2. **Chain `--snap` to save turns.** When an action changes the UI (e.g., clicking a button or opening a modal), append `--snap` (`chrome-ctl click 5 --snap`) to perform the action and get the updated element map in a single command.
3. **Use Visuals Strategically.** Only call `chrome-ctl shot` when:
   - Text snapshot (`snap`) is ambiguous (e.g., icon-only buttons, canvas UI, Instagram story editors).
   - You need to verify visual layout or provide proof of task completion.
   - Always prefer `chrome-ctl shot --som` (Set-of-Mark) so visual elements have matching `[ID]` badges drawn on them.
4. **Check Macros First.** Before starting a multi-step administrative or outreach task, run `chrome-ctl macro list` to see if a learned workflow already exists.

---

## 1. Standard Operating Loop (Observe -> Act -> Verify)

### Step 1: Open or Attach to Tab
```bash
chrome-ctl tabs
chrome-ctl open "[https://www.linkedin.com](https://www.linkedin.com)"

```

### Step 2: Distill Interactive Elements (`snap`)

```bash
chrome-ctl snap

```

*Output Example:*

```text
TAB [2] Feed | LinkedIn | [https://www.linkedin.com/feed/](https://www.linkedin.com/feed/)
VIEWPORT: 0-900px of 4200px (21%)
[1] <input:text> "Search" () @310,28
[2] <button> "Start a post" @540,135
[3] <a:link> "My Network" @420,28
[4] <a:link> "Messaging" @610,28

```

### Step 3: Execute Humanized Actions

All `click`, `type`, and `scroll` commands automatically simulate human Bezier mouse curves, variable typing cadence, and natural delays.

```bash
# Click "Start a post" and immediately get the modal snapshot
chrome-ctl click 2 --snap

# Type content with human cadence into element [12]
chrome-ctl type 12 "Just shipped a new AI automation pipeline for our clients. Here is how we cut latency by 60%..."

# Attach media if needed
chrome-ctl upload 14 "/path/to/graphic.webp"

# Submit post
chrome-ctl click 18

```

---

## 2. Social Media Pacing & Anti-Detection Rules (Instagram, LinkedIn, Facebook)

When operating on social networks for outreach, connection building, or posting, strictly follow these behavioral guardrails:

1. **Context Warming:** Before sending a direct message or connection request on LinkedIn/Instagram, scroll the target's profile first (`chrome-ctl scroll down 0.6`), pause (`chrome-ctl wait 2500`), and optionally view a post. Do not jump straight to URL -> Click Connect -> Close Tab in under 2 seconds.
2. **Rate Pacing:** Insert randomized human pauses (`chrome-ctl wait 3000-7000`) between outreach iterations.
3. **Draft Verification:** Before clicking final "Post" or "Send" on high-stakes outreach, run `chrome-ctl shot --gray -o /tmp/verify.webp` if you need to confirm formatting or media attachment.

---

## 3. Google OAuth SSO & Directory Registrations

Because `chrome-ctl` runs in the user's authenticated Chrome profile, you can register the user's business (e.g., web development studio, SaaS listings, Clutch, DesignRush, GoodFirms, local directories) without inventing passwords whenever Google Sign-In is available:

1. **Attempt Google SSO First:**
Look for `"Continue with Google"` or `"Sign in with Google"` in `chrome-ctl snap`.
```bash
chrome-ctl click <google_btn_id>
chrome-ctl sso

```


`chrome-ctl sso` automatically handles the Google account picker popup/redirect and returns once the parent site logs in.
2. **Fast Form Filling (`--forms`):**
On registration forms, filter noise by running:
```bash
chrome-ctl snap --forms

```


This returns only `<input>`, `<select>`, `<textarea>`, and `<button:submit>` elements so you can map company details (Name, Website URL, Description, Location, Logo upload) with minimal tokens.

---

## 4. Visual Snapshots & Task Verification Recordings

### Taking Token-Optimized Screenshots

* **Standard Verification:** `chrome-ctl shot -o /tmp/proof.webp` (outputs ~40KB compressed WebP).
* **Element Disambiguation:** `chrome-ctl shot --som -o /tmp/som.webp` (draws `[ID]` numbers on top of every clickable element in the image).

### Recording a Task Execution (Optimized Video / Storyboard)

When executing an important task (e.g., publishing a campaign, submitting a company listing, or debugging a UI flow) where a visual audit trail is needed:

1. **Start Recording before critical steps:**
```bash
chrome-ctl rec start --fps 2

```


2. **Perform the workflow actions (`click`, `type`, `scroll`).**
3. **Stop and export:**
* **For AI Agent Self-Review (Lowest Token Cost):** Export as a stitched `3x4` storyboard contact sheet (single WebP image showing the whole sequence):
```bash
chrome-ctl rec stop --grid -o /tmp/task_storyboard.webp

```


* **For Human User Review:** Export as an ultra-compact 2fps WebM video:
```bash
chrome-ctl rec stop -o /tmp/task_recording.webm

```




4. **Post-Task Snapshot:** Always take a final confirmation snapshot or screenshot after completing a state-changing task (e.g., registration confirmed, post live) to verify success before reporting back.

---

## 5. Learning & Reusing Workflows (Macro Engine)

Whenever you perform a repeatable task (e.g., *"Register company on a business directory"*, *"Send LinkedIn connection request + note to a founder"*, *"Post update to Instagram"*), turn it into a reusable zero-token macro:

### How to Record & Teach Yourself a New Macro

1. **Start Macro Recording:**
```bash
chrome-ctl macro start linkedin_founder_outreach

```


2. **Execute the task normally** using `open`, `snap`, `click`, `type`, `wait`.
3. **Verify success** with `chrome-ctl snap` or `chrome-ctl shot`.
4. **Save & Parameterize:**
Replace dynamic values (like profile URL and custom message) with named parameters:
```bash
chrome-ctl macro save --params "profile_url,custom_note"

```


This saves the resilient selector path and humanized timing rules to `~/.chrome-ctl/macros/linkedin_founder_outreach.json`.

### How to Reuse a Learned Macro (0 LLM Tokens)

Next time you need to run the same task for a batch of leads or directories:

```bash
chrome-ctl macro list
chrome-ctl macro run linkedin_founder_outreach --vars '{"profile_url": "[https://linkedin.com/in/example](https://linkedin.com/in/example)", "custom_note": "Hi Alex, loved your recent post on..."}'

```

*If a website layout changes and a macro step fails, `chrome-ctl` will pause at the exact failing step and output a fresh `snap` so you can take over, fix the step, and update the macro.*

```

---

## 7. Implementation Roadmap (Suggested Tech Stack)

To keep the tool fast, zero-cost, and local-first on macOS/Linux:
1. **CLI & Local Daemon (`Go` or `Bun/TypeScript`):**
   * **Go** compiles to a single instant-startup binary (`chrome-ctl`) with zero runtime overhead, using `gorilla/websocket` for the extension bridge and `libwebp` / `ffmpeg` bindings for image/video compression.
2. **Chrome Extension (Manifest V3, Vanilla TS):**
   * `permissions`: `["activeTab", "tabs", "debugger", "scripting", "storage"]`
   * `host_permissions`: `["<all_urls>"]`
   * Keeps a persistent WebSocket connection (`ws://127.0.0.1:9229`) alive via MV3 offscreen document or periodic heartbeat alarm.


