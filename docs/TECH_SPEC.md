# Technical Specification: `chrome-ctl`

**Document Version:** 1.0  
**Status:** Approved Architecture  
**Author:** AI System Architecture Team  

---

## 1. Architectural Decisions & Tech Stack Rationale

To fulfill the requirements in [PRD.md](file:///Users/rajeshshrirao/Desktop/Chrome-ctl/PRD.md) (extreme token efficiency, zero detection, sub-10ms CLI responsiveness, and zero-cost local execution), we have made the following definitive design decisions:

### 1.1 CLI & Local Daemon: Go (v1.23+)
* **Why Go over Node/Bun or Python?**
  * **Instant Cold-Start Latency:** Autonomous AI agents (Hermes, Claude Code) invoke CLI commands repeatedly in tight loops. Go binaries initialize in `<8ms` compared to Node/Bun (`150ms–300ms`) or Python (`100ms–250ms`).
  * **Single Standalone Static Binary:** No `node_modules`, no npm runtime dependencies, and no Python environment conflicts. Users or agents can install with a single binary download or `go install`.
  * **Native Daemon Concurrency:** Go’s goroutine and channel model natively handles high-concurrency WebSocket messaging, media stream decimation, and macro execution with `<20MB` resident memory.
  * **CLI Framework:** Built using `github.com/spf13/cobra` for command routing and flags.

### 1.2 IPC Bridge: Local WebSocket (`ws://127.0.0.1:9229/ws`)
* **Why WebSocket over Chrome Native Messaging?**
  * Native Messaging requires installing OS-specific JSON host manifests into system directories (`~/Library/Application Support/Google/Chrome/NativeMessagingHosts/`), which introduces brittle installer scripts and permission failures.
  * A local WebSocket server on `127.0.0.1:9229` requires zero OS-level configuration.
* **Security Model:**
  * To prevent malicious web pages on the user's browser or local processes from connecting, the daemon generates a 64-character cryptographically secure token stored in `~/.chrome-ctl/auth_token` (`chmod 0600`).
  * The WebSocket handshake requires `Authorization: Bearer <TOKEN>` or `?token=<TOKEN>`. Unauthenticated connections are dropped instantly.
  * The extension obtains the token through `POST /pair`, restricted to loopback peers/hosts, the `X-Chrome-Ctl-Pair: 1` header, and a `chrome-extension://` (or absent) `Origin`. This keeps the zero-config install without exposing the token to web pages.

### 1.3 Chrome Extension: Manifest V3 (Vanilla TypeScript + Vite/esbuild)
* **Offscreen Document for Persistent Connection:** Manifest V3 background service workers terminate after 30 seconds of inactivity. We employ a dedicated, hidden `offscreen.html` document with `chrome.offscreen` API. Offscreen documents are **not** subject to the 30-second service worker suspension, providing an unbroken, persistent WebSocket connection.
* **Dual-Layer Automation:**
  * **Content Script:** Inspects visible DOM, filters interactive elements, computes viewport coordinates, and outputs numeric IDs (`[1]`, `[2]`).
  * **Debugger API (`chrome.debugger`):** Dispatches trusted hardware input events (`Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`) with `event.isTrusted = true` to evade bot heuristics.

### 1.4 Media & Vision Pipeline
* **WebP Encoding:** Handled inside the Go daemon using native image processing (`golang.org/x/image` or fast WebP bindings). Downscales images to max width 1024px with Quality 65.
* **Set-of-Mark (SoM):** Content script calculates precise bounding boxes during snapshot; the daemon overlays SVG/vector numeric badges onto the screenshot buffer before saving.
* **Video Recording:** Uses `chrome.tabCapture` stream or `Page.screencastFrame` inside the extension, downsampled to 1–2 FPS WebM (VP9) to guarantee minimal token footprint.

---

## 2. System Architecture & Topology

```
+---------------------------------------------------------------------------------+
|                               AI Agent Runtime                                  |
|         (Hermes Agent / Claude Code / Subprocess invoking `chrome-ctl`)        |
+---------------------------------------+-----------------------------------------+
                                        |  CLI Invocations (stdout: <300 tokens)
                                        v
+---------------------------------------------------------------------------------+
|                       `chrome-ctl` Go Binary & Daemon                           |
|                                                                                 |
|  [ CLI Command Parser ] (Cobra)                                                 |
|         |                                                                       |
|         v                                                                       |
|  [ Daemon Client / IPC Connector ]                                              |
|         | (Sends command to background daemon via Unix domain socket or WS)     |
|         v                                                                       |
|  [ Daemon Core Engine ]                                                         |
|    - WebSocket Server (gorilla/websocket) @ 127.0.0.1:9229                      |
|    - Auth Token Validator (~/.chrome-ctl/auth_token)                            |
|    - Media Compressor & SoM Renderer (WebP 65%, 2fps WebM decimation)          |
|    - Macro & Playbook Engine (~/.chrome-ctl/macros/*.json)                      |
+---------------------------------------+-----------------------------------------+
                                        |  Authenticated WebSocket
                                        |  (JSON-RPC 2.0 frames)
                                        v
+---------------------------------------------------------------------------------+
|                     Chrome Extension (Manifest V3)                              |
|                                                                                 |
|  +-----------------------------+       +-------------------------------------+  |
|  |     Offscreen Document      | <---> |      Background Service Worker      |  |
|  | (Persistent WS Connection)  |       | (Tab Manager, chrome.debugger host) |  |
|  +-----------------------------+       +-------------------+-----------------+  |
|                                                            |                    |
|                                        chrome.tabs.sendMessage / debugger       |
|                                                            v                    |
|  +---------------------------------------------------------------------------+  |
|  |                          Active Browser Tab                               |  |
|  |  +-------------------------------+     +-------------------------------+  |  |
|  |  |    Content Script Bridge      |     |     CDP Input Dispatch        |  |  |
|  |  | - Viewport Element Indexer    |     | - Mouse Click (isTrusted=true)|  |  |
|  |  | - Label / ARIA Distillation   |     | - Cadence Typing              |  |  |
|  |  | - Form Map Extraction         |     | - Momentum Smooth Scroll      |  |  |
|  |  +-------------------------------+     +-------------------------------+  |  |
|  +---------------------------------------------------------------------------+  |
+---------------------------------------------------------------------------------+
```

---

## 3. Directory Layout

```
/Users/rajeshshrirao/Desktop/Chrome-ctl/
├── PRD.md                       # Product Requirements Document
├── TECH_SPEC.md                 # System Architecture & Technical Specifications (This File)
├── PROTOCOL.md                  # Wire Protocol Specification (JSON Schemas)
├── MV3_LIFECYCLE_SPEC.md        # Extension Lifecycle & Debugger Architecture
├── IMPLEMENTATION_PLAN.md       # Phased Execution Roadmap & Milestones
├── Makefile                     # Build & developer tasks
├── go.mod                       # Go module definition
├── go.sum                       # Go dependencies
├── cmd/
│   └── chrome-ctl/
│       └── main.go              # CLI entry point (Cobra commands)
├── internal/
│   ├── cli/                     # Cobra subcommands, one file per command
│   │   ├── root.go              # Command registration
│   │   ├── rpc.go               # JSON-RPC client & error-to-message mapping
│   │   ├── snap.go read.go      # Observation (--deep/--max-elements, --tables/--links)
│   │   ├── click.go clickat.go  # Element click & viewport CSS-pixel click
│   │   ├── type.go press.go     # Typing with read-back verification, key presses
│   │   ├── eval.go describe.go href.go url.go   # Escape hatch & cheap inspection
│   │   ├── wait.go net.go       # Network/text waits, CDP request history
│   │   ├── shot.go rec.go       # Screenshots (CDP fallback) & recordings
│   │   └── bulk.go              # Many-page extraction with a manifest
│   ├── daemon/                  # Background WebSocket server, connection manager, dispatch
│   ├── protocol/                # Shared message types & serialization
│   ├── media/                   # WebP compression, SoM badge drawing, WebM grid
│   ├── humanizer/               # Bézier math, typing cadence, Gaussian offsets
│   └── macro/                   # Playbook recorder, parameterizer, and replayer
├── extension/                   # Manifest V3 Chrome Extension
│   ├── manifest.json            # MV3 configuration
│   ├── package.json             # Extension build dependencies (esbuild)
│   ├── tsconfig.json            # TypeScript configuration
│   ├── build.mjs                # esbuild bundling → extension/dist (gitignored)
│   ├── src/
│   │   ├── background/          # Service worker & privileged helpers
│   │   │   ├── service_worker.ts  # RPC routing, snapshot-epoch cache, waits
│   │   │   ├── actions.ts         # Click/type sequencing, read-back verification
│   │   │   ├── capture.ts         # captureVisibleTab + CDP screenshot fallback
│   │   │   ├── debugger.ts        # chrome.debugger attach/detach controller
│   │   │   ├── netlog.ts          # CDP Network ring log & quiet tracker
│   │   │   ├── resolver.ts        # Selector/id/semantic target resolution
│   │   │   └── humanizer.ts sso.ts windows.ts faults.ts
│   │   ├── offscreen/           # Offscreen document (persistent WS connection)
│   │   │   ├── offscreen.html
│   │   │   └── offscreen.ts
│   │   ├── content/             # In-page DOM distillation & interaction
│   │   │   ├── indexer.ts       # Request routing, global ID epoch, docId guard
│   │   │   ├── dom.ts           # Candidate selection, labels, state tokens, rects
│   │   │   ├── interact.ts      # Idle/text waits, focus, input helpers
│   │   │   ├── read.ts          # Markdown, table, and link extraction
│   │   │   └── forms.ts         # Fast form distillation (--forms)
│   │   ├── popup/               # Extension popup (shows daemon status, token input)
│   │   │   ├── popup.html
│   │   │   └── popup.ts
│   │   └── shared/              # Shared types with the Go protocol
│   │       ├── protocol.ts      # RPC params/results (mirrors internal/protocol)
│   │       └── content.ts       # Content-script IPC message shapes
│   ├── tests/                   # node --test suites (content, epoch, netlog, read modes)
│   └── icons/                   # Extension icons (16, 48, 128)
├── testdata/                    # Local HTML fixtures (e.g. spa.html) for E2E runs
└── tests/
    ├── fixtures/                # Local HTML mock pages (forms, dynamic lists, auth)
    └── e2e/                     # Go integration tests
```

---

## 4. Key Subsystem Specifications

### 4.1 CLI Command Execution Flow
1. User/Agent executes: `chrome-ctl click 2 --snap`
2. `chrome-ctl` binary starts up:
   * Checks if daemon is running on `127.0.0.1:9229`.
   * If not running, launches daemon in background (`chrome-ctl daemon --start`) and waits up to 2 seconds for readiness.
3. CLI sends JSON-RPC command over local TCP/WS client to daemon.
4. Daemon routes command to Extension via active WebSocket.
5. Extension:
   * Resolves target element `[2]` from current tab's active index map.
   * Dispatches Bézier mouse move + click via `chrome.debugger` (`Input.dispatchMouseEvent`).
   * Because `--snap` was requested, immediately runs content script indexer.
6. Daemon receives response and prints compact stdout:
   ```text
   OK: Clicked [2] <button> "Start a post".
   SNAP 3 | TAB [2] Feed | LinkedIn | https://www.linkedin.com/feed/
   VIEWPORT: 0-900px of 4200px (21%)
   [1] <input:text> "Search" () @310,28
   [2] <div:textbox> "What do you want to talk about?" @540,135
   ```
7. CLI exits with code `0`. Elapsed time: `<120ms`.

### 4.2 Interactive Node Identification Algorithm (`snap`)
To achieve `<450 tokens` per snapshot:
1. Scan DOM for interactive candidates:
   * Native interactives: `a[href]`, `button`, `input:not([type="hidden"])`, `textarea`, `select`, `[role="button"]`, `[role="link"]`, `[role="checkbox"]`, `[role="menuitem"]`, `[role="tab"]`, `[contenteditable="true"]`.
2. Visibility Cull:
   * Filter out `display: none`, `visibility: hidden`, `opacity: 0`, and elements with bounding width/height $< 4\text{px}$.
   * Filter out elements obscured by modal backdrops or out of viewport (unless `--all` is passed).
3. Deduplication & Hierarchy Flattening:
   * If a `<button>` contains a nested `<span>`, index the `<button>`, ignore the inner text span.
4. Assign **globally unique** integer IDs: the snapshot starts at the current `nextId` and advances it, so IDs are never reused within a document. (The earlier `1..n`-per-snap scheme aliased old IDs onto new elements — see `MV3_LIFECYCLE_SPEC.md` §4.1.)
5. Merge element references into `window.__chromeCtlIndex = new Map<number, Element>()`, pruning `!isConnected` entries first.
6. Emit `docId`, `snapshotId`, and `omitted` alongside the text. The service worker prefixes the formatted block with `SNAP <snapshotId> | `, rejects actions carrying a stale `expectedDocId`, and appends an `OMITTED: n candidates` footer when `--max-elements` culls anything.
7. Format output into single-line tokens:
   `[ID] <TAG:type> "Label/Text" (value/state) @x,y`

### 4.3 Humanizer Engine Architecture
Dispatches events via CDP (`chrome.debugger`):
* **Trajectory:** Cubic Bézier curve between current mouse position $(x_0, y_0)$ and target $(x_1, y_1)$. Two control points are randomized with curvature $K \sim \mathcal{N}(0.2, 0.05)$.
* **Velocity:** Fitts's Law distribution (fast acceleration, decelerates when approaching target within $25\text{px}$).
* **Target Point:** Gaussian scatter around bounding box center with standard deviation $\sigma = 0.2 \times \min(w, h)$.
* **Typing Delay:** Inter-key delays sampled from log-normal distribution ($\mu = 65\text{ms}, \sigma = 25\text{ms}$). Bigram acceleration applied to common pairs (`th`, `er`, `in`).

---

## 5. Security & Isolation Invariants

1. **Localhost Binding Only:** Daemon listens exclusively on `127.0.0.1` (never `0.0.0.0`).
2. **Mandatory Handshake Token:** Unauthenticated WebSocket connections receive HTTP `401 Unauthorized` and are dropped immediately.
3. **No Credential Exfiltration:** CLI and Daemon never log, store, or transmit passwords, tokens, or raw cookies to external servers. All operations occur strictly locally on the user's machine.
4. **Target Tab Guard:** Clicks and keyboard inputs cannot be dispatched to tabs that are not explicitly targeted or active.
