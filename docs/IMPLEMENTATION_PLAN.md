# Implementation Plan: `chrome-ctl`

**Document Version:** 1.0  
**Target Completion:** Phased Execution (Phase 1 through Phase 6)  
**Verification Protocol:** Automated tests + Local HTML fixtures  

---

## Phase 1: Foundation, Daemons & Authenticated IPC Bridge

### Goal
Establish the Go CLI skeleton, daemon WebSocket server, security token generation, and the Chrome Extension with its persistent offscreen WebSocket client.

### Tasks
- [x] **1.1 Project & Go Module Setup**
  - Initialize Go module `github.com/rajeshshrirao/chrome-ctl` (Go 1.23+).
  - Setup Cobra CLI structure in `cmd/chrome-ctl/main.go` and `internal/cli/`.
  - Add basic subcommands: `status`, `daemon`, `version`.
- [x] **1.2 Daemon WebSocket Server & Auth Manager**
  - Implement `internal/daemon/server.go` using `gorilla/websocket`.
  - Implement `internal/daemon/auth.go`:
    - Generate or load 64-char hex auth token from `~/.chrome-ctl/auth_token` (`chmod 0600`).
    - Validate `?token=...` during WebSocket upgrade.
  - Implement 15-second ping/pong heartbeat handling.
- [x] **1.3 Chrome Extension Scaffolding (Manifest V3)**
  - Setup TypeScript build with `esbuild` or `tsup` in `extension/`.
  - Create `extension/manifest.json` with permissions: `["activeTab", "tabs", "debugger", "scripting", "storage", "offscreen"]`.
  - Implement `src/offscreen/offscreen.ts`:
    - Establishes persistent WebSocket connection to `ws://127.0.0.1:9229/ws?token=...`.
    - Handles automatic reconnection with exponential backoff on disconnect.
    - Sends periodic heartbeats.
  - Implement `src/background/service_worker.ts`:
    - Ensures offscreen document is spawned on launch.
    - Listens for messages forwarded by the offscreen document.
  - Implement `src/popup/popup.html` and `popup.ts` to display daemon connection status and allow manual token entry if needed.

### Exit Criteria & Verification
```bash
# 1. Start daemon
./bin/chrome-ctl daemon --start

# 2. Load unpacked extension in Google Chrome (Developer mode)
# 3. Check status from terminal
./bin/chrome-ctl status
# Output:
# Daemon: RUNNING (127.0.0.1:9229)
# Chrome Extension: CONNECTED
# Active Tabs: 3
```

---

## Phase 2: Observation & DOM Distillation (`tabs`, `open`, `snap`, `read`)

### Goal
Enable tab navigation, clean tab querying, and token-minimal interactive element indexing (`snap`) along with readable content extraction (`read`).

### Tasks
- [x] **2.1 Tab Management RPCs**
  - Implement `tab.list`, `tab.open`, `tab.switch` in extension service worker.
  - Implement CLI commands:
    - `chrome-ctl tabs`
    - `chrome-ctl open <url> [--new]`
    - `chrome-ctl switch <tab_id>`
- [x] **2.2 In-Page Content Script Indexer (`src/content/indexer.ts`)**
  - Implement interactive element query selector (`a[href]`, `button`, `input`, `textarea`, `select`, `[role="button"]`, etc.).
  - Implement visibility filter: discard elements with `display: none`, `visibility: hidden`, `opacity: 0`, or $w, h < 4\text{px}$.
  - Viewport bounds check: discard elements outside current viewport unless `--all` is set.
  - Contiguous integer ID assignment (`[1]`, `[2]`, `[3]`).
  - Flattening of nested interactive targets (e.g., `<button><span>Text</span></button>`).
  - Output formatting strictly matching PRD schema:
    `[ID] <TAG:type> "Label/Text" (value/state) @x,y`
- [x] **2.3 Form Mode Filter (`--forms`)**
  - Implement `src/content/forms.ts` to filter strictly for inputs, selects, textareas, and submit buttons.
- [x] **2.4 Content Extractor (`page.read`)**
  - Implement readability/markdown conversion for page or target container ID.
  - Wire CLI command: `chrome-ctl read [<id_or_selector>]`.

### Exit Criteria & Verification
```bash
./bin/chrome-ctl open "https://news.ycombinator.com"
./bin/chrome-ctl snap
# Verify output token count is < 300 tokens and prints structured IDs:
# [1] <a:link> "Hacker News" @18,4
# [2] <a:link> "new" @120,4
# [3] <a:link> "past" @160,4
```

---

## Phase 3: Humanized Actions & CDP Input Dispatch (`click`, `type`, `press`, `scroll`, `upload`)

### Goal
Implement trusted hardware events via `chrome.debugger` combined with human biometrics (Bézier curves, Fitts's Law, cadence typing, momentum scroll).

### Tasks
- [x] **3.1 `chrome.debugger` Lifecycle Controller**
  - Implement lazy attach/detach for target tabs in `src/background/debugger.ts`.
  - Handle auto-reconnection on tab navigation.
- [x] **3.2 Humanizer Engine (`internal/humanizer/` & Extension)**
  - Implement cubic Bézier trajectory generation between starting position and target coordinates.
  - Add Gaussian coordinate jitter ($\sigma = 0.2 \times \min(w, h)$) so clicks never hit exact center.
  - Implement Fitts's Law acceleration and deceleration curves.
  - Implement cadence typing: log-normal inter-key delays ($\mu = 65\text{ms}, \sigma = 25\text{ms}$), bigram speedups, and typo-burst simulation with backspace.
- [x] **3.3 Action Commands**
  - Implement `chrome-ctl click <id> [--snap]`
  - Implement `chrome-ctl type <id> "<text>" [--enter] [--typo-rate 0.02] [--snap]`
  - Implement `chrome-ctl press <key>` (`Enter`, `Tab`, `Escape`, `ArrowDown`)
  - Implement `chrome-ctl scroll <down|up> [amount]` (smooth momentum wheel ticks)
  - Implement `chrome-ctl upload <id> <file_path>` (file input handler)
  - Implement `chrome-ctl wait <ms|idle>`

### Exit Criteria & Verification
```bash
# Verify on local test form fixture:
./bin/chrome-ctl open "http://localhost:8080/test_form.html"
./bin/chrome-ctl snap
./bin/chrome-ctl click 2 --snap
./bin/chrome-ctl type 3 "Autonomous AI Agent" --enter
# Verify in browser console that event.isTrusted === true and form state updates.
```

---

## Phase 4: Vision & Media Token Optimization (`shot`, `rec`)

### Goal
Deliver token-optimized visual verification artifacts (compressed WebP, Set-of-Mark overlays, and 1–2 FPS WebM / 3x4 contact sheets).

### Tasks
- [x] **4.1 Screen Capture & Set-of-Mark (SoM)**
  - Capture active viewport via `chrome.tabs.captureVisibleTab`.
  - Coordinate mapping: extract bounding boxes from content script indexer.
  - Draw high-contrast numeric badges (`[1]`, `[2]`) at element positions if `--som` is set.
- [x] **4.2 Image Compression Pipeline (`internal/media/image.go`)**
  - Downscale image width to max `1024px` maintaining aspect ratio.
  - Encode to WebP Quality 65.
  - Support grayscale conversion (`--gray`) to minimize tokens for text verification.
  - Implement CLI command: `chrome-ctl shot [--som] [--gray] [-o path]`.
- [x] **4.3 Low-FPS Recording & Storyboard Contact Sheets**
  - Implement frame capture (1–2 FPS) during recording session in `internal/media/video.go`.
  - Stitch sequential frames into a $3 \times 4$ contact sheet WebP image (`--grid`).
  - Encode video to low-bitrate WebM (VP9).
  - Implement CLI commands:
    - `chrome-ctl rec start [--fps 2]`
    - `chrome-ctl rec stop [--grid] [-o path]`

### Exit Criteria & Verification
```bash
./bin/chrome-ctl shot --som -o /tmp/som_test.webp
# Verify file size is between 25KB and 60KB and numbers match snap IDs.
```

---

## Phase 5: Google SSO Interceptor & Macro Replay Engine (`sso`, `macro`)

### Goal
Provide zero-touch Google OAuth authentication and deterministic zero-token local macro recording/replaying.

### Tasks
- [x] **5.1 Google OAuth Popup Interceptor (`chrome-ctl sso`)**
  - Service worker detects popup window or tab navigation to `accounts.google.com`.
  - Identifies target Google account (or picks default active account).
  - Automatically clicks "Allow / Continue" and waits for popup closure and parent tab redirect.
- [x] **5.2 Macro Recorder (`chrome-ctl macro start <name>`)**
  - Action logger stores executed CLI steps with multi-tiered resilient selectors (ARIA label, text content, CSS selector, relative XPath).
- [x] **5.3 Macro Parameterizer & Storage (`chrome-ctl macro save`)**
  - Parameterize recorded literals into `{{variable_name}}` templates.
  - Save schema to `~/.chrome-ctl/macros/<name>.json`.
- [x] **5.4 Zero-Token Macro Replayer (`chrome-ctl macro run`)**
  - Replays steps locally through the Go daemon with parameter substitution.
  - Pauses and returns a clean `snap` if an element selector fails, allowing the LLM to intervene.

### Exit Criteria & Verification
```bash
./bin/chrome-ctl macro start lead_search
./bin/chrome-ctl open "https://example.com/directory"
./bin/chrome-ctl snap
./bin/chrome-ctl type 1 "Software Agencies" --enter
./bin/chrome-ctl macro save --params "search_term"

# Replay with zero tokens:
./bin/chrome-ctl macro run lead_search --vars '{"search_term": "Design Studios"}'
```

---

## Phase 6: Packaging, Agent Skills (`SKILL.md`) & Hardening

### Tasks
- [x] **6.1 Makefile & Multi-Platform Build**
  - Build scripts for macOS (Apple Silicon `darwin-arm64`, Intel `darwin-amd64`) and Linux (`linux-amd64`).
- [x] **6.2 Agent Skill Definition**
  - Deploy `SKILL.md` for AI agent environments (Hermes, Claude Code, OpenCode).
- [x] **6.3 Integration Test Suite**
  - Automated fixtures testing complex UI scenarios: Shadow DOM, nested iframes, infinite scroll, and modal dialogs.
