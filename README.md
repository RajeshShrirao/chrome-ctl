# chrome-ctl

> **Ultra-low token browser automation & anti-detection bridge for autonomous AI agents.**  
> Connects AI agents (Hermes, Claude Code, OpenCode) directly to the user's authenticated Google Chrome browser with zero-detection session reuse and extreme token efficiency.

---

## ⚡ Highlights

* **Zero-Detection Session Reuse:** Runs via a lightweight Manifest V3 Chrome Extension inside the user's primary, authenticated browser profile. Inherits active cookies, Google SSO sessions, and real hardware fingerprints.
* **Extreme Token Efficiency:** Replaces bloated HTML/DOM dumps (>8,500 tokens) with compact numeric element maps (`snap`: ~95–350 tokens).
* **Set-of-Mark (SoM) & Vision Compression:** Automatic WebP downscaling (Quality 65, <40KB) and Set-of-Mark numeric badges for Vision LLMs.
* **Contact Sheet Storyboard Recording:** Compiles 15-second task executions into a single $3 \times 4$ WebP contact sheet (~340 tokens total).
* **Human-Like Biometrics:** Generates cubic Bézier mouse trajectories, Fitts's Law velocity curves, Gaussian click offsets, and natural typing cadence via trusted CDP events (`event.isTrusted = true`).
* **Zero-Token Macro Replay Engine:** Learn workflows once, parameterize them with templates (`{{company_name}}`), and replay them locally through the daemon consuming **0 LLM tokens**.

---

## 📚 Architectural & Technical Documentation

All foundational and module specifications are organized in the [`docs/`](./docs) directory:

| Document | Purpose & Scope |
| :--- | :--- |
| **[`docs/PRD.md`](./docs/PRD.md)** | Product Requirements Document: Vision, command table, token budget, and agent skill definition. |
| **[`docs/TECH_SPEC.md`](./docs/TECH_SPEC.md)** | Technical Specification: Architecture topology, Go + MV3 stack rationale, security model, and directory layout. |
| **[`docs/PROTOCOL.md`](./docs/PROTOCOL.md)** | Wire Protocol Specification: JSON-RPC 2.0 schemas for WebSocket IPC (`ws://127.0.0.1:9229/ws`). |
| **[`docs/MV3_LIFECYCLE_SPEC.md`](./docs/MV3_LIFECYCLE_SPEC.md)** | Manifest V3 Lifecycle: Offscreen document WebSocket host, service worker awakening, and CDP debugger management. |
| **[`docs/IMPLEMENTATION_PLAN.md`](./docs/IMPLEMENTATION_PLAN.md)** | Phased Execution Roadmap: 6-phase development checklist with verification tests. |
| **[`docs/HUMANIZER_SPEC.md`](./docs/HUMANIZER_SPEC.md)** | Humanizer Engine Specification: Cubic Bézier kinematics, Fitts's Law, cadence typing, and anti-bot evasion. |
| **[`docs/MEDIA_PIPELINE.md`](./docs/MEDIA_PIPELINE.md)** | Media & Vision Pipeline: WebP compression, Set-of-Mark overlays, and $3 \times 4$ storyboard grid generation. |
| **[`docs/MACRO_SCHEMA.md`](./docs/MACRO_SCHEMA.md)** | Macro & Playbook Specification: Self-learning workflow engine, resilient multi-tier selectors, and replay schema. |

---

## 🛠️ System Architecture

```
+-------------------------------------------------------------------------+
|                           AI Agent (Hermes)                             |
|       Reads compact stdout (<500 tokens/turn) & executes CLI cmds       |
+------------------------------------+------------------------------------+
                                     |
                                     v  (Subprocess / Bash execution)
+-------------------------------------------------------------------------+
|                        `chrome-ctl` CLI Binary                          |
|   - Stateless command client & local daemon (Go v1.23+)                 |
|   - Media Optimizer (WebP 65%, Set-of-Mark, 2fps WebM / Grid)          |
|   - Playbook / Macro Storage (`~/.chrome-ctl/macros/*.json`)            |
+------------------------------------+------------------------------------+
                                     |
                                     v  (Local WebSocket: ws://127.0.0.1:9229)
+-------------------------------------------------------------------------+
|                    Chrome Extension (Manifest V3)                       |
|   - Offscreen Document (Persistent WS client host)                      |
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

## 🚀 Quick CLI Cheat Sheet

```bash
# Check status
chrome-ctl status

# Tabs & Navigation
chrome-ctl tabs
chrome-ctl open "https://news.ycombinator.com"

# Observation
chrome-ctl snap               # Distill viewport interactive elements
chrome-ctl snap --forms       # Distill form inputs & submit buttons only
chrome-ctl read [<id>]        # Extract readable markdown

# Visuals
chrome-ctl shot --som -o /tmp/view.webp   # Set-of-Mark WebP screenshot
chrome-ctl rec start --fps 2              # Start low-framerate recording
chrome-ctl rec stop --grid -o /tmp/seq.webp # Export 3x4 storyboard grid

# Humanized Actions (event.isTrusted = true)
chrome-ctl click <id> [--snap]
chrome-ctl type <id> "<text>" [--enter] [--typo-rate 0.02]
chrome-ctl scroll down 0.8
chrome-ctl press Enter

# Auth & Macros
chrome-ctl sso                # Auto-complete Google OAuth account picker
chrome-ctl macro list
chrome-ctl macro run <name> --vars '{"param": "value"}'
```

---

## Local Development (Phases 1–5)

Prerequisites: Go 1.23+, Node.js 22+, and Google Chrome 116+.

```bash
# Build the CLI
mkdir -p bin
go build -o bin/chrome-ctl ./cmd/chrome-ctl

# Build the extension (load-unpacked target: extension/dist)
cd extension
npm install
npm run typecheck
npm run build
cd ..

# Start the daemon; the token is printed only on first run
./bin/chrome-ctl daemon --start

# Verify
./bin/chrome-ctl status
```

Load `extension/dist` from `chrome://extensions` (Developer mode → Load unpacked), open the extension popup, paste the token, and click **Save**. The popup should show **Connected**, and `chrome-ctl status` should report **CONNECTED** with the live tab count.

With the extension loaded, exercise the observation commands:

```bash
./bin/chrome-ctl open "https://news.ycombinator.com"
./bin/chrome-ctl tabs
./bin/chrome-ctl snap            # viewport interactive elements
./bin/chrome-ctl snap --all      # include off-screen elements
./bin/chrome-ctl snap --forms    # form fields and submit buttons only
./bin/chrome-ctl read            # readable markdown for the page
./bin/chrome-ctl read 3          # markdown for snapshot element [3]
```

Humanized actions dispatch trusted CDP input (`event.isTrusted === true`). The first action on a tab attaches `chrome.debugger`, so Chrome shows its yellow "started debugging this browser" infobar — that is expected; leave it attached.

```bash
./bin/chrome-ctl click 2                 # Bézier move + Gaussian-offset click
./bin/chrome-ctl click 2 --snap          # click, then return the fresh snapshot
./bin/chrome-ctl type 3 "Autonomous AI Agent" --enter
./bin/chrome-ctl press Enter
./bin/chrome-ctl scroll down             # 0.8 viewports, momentum wheel ticks
./bin/chrome-ctl upload 5 ./logo.png     # attach a local file to a file input
./bin/chrome-ctl wait 2500               # human pause
./bin/chrome-ctl wait 3000-7000          # random pause in range
./bin/chrome-ctl wait "#results"         # wait for a selector
./bin/chrome-ctl wait idle               # wait for DOM quiet + document complete
```

`wait idle` observes DOM mutations and `document.readyState`, not network activity: content scripts cannot see the page's network layer.

Visual artifacts are compressed to WebP (max width 1024, quality 65) with `--som` badge numbers that match `snap` element IDs. Recordings capture at 1–2 FPS; `--grid` exports a 3x4 storyboard without extra dependencies. WebM export additionally requires `ffmpeg` on `PATH` (or `CHROME_CTL_FFMPEG=/path/to/ffmpeg`).

```bash
./bin/chrome-ctl shot --som -o /tmp/view.webp     # badges over interactive elements
./bin/chrome-ctl shot --gray -o /tmp/text.webp    # grayscale for text verification
./bin/chrome-ctl rec start --fps 2
./bin/chrome-ctl rec stop --grid -o /tmp/seq.webp # storyboard contact sheet
./bin/chrome-ctl rec start --fps 1
./bin/chrome-ctl rec stop -o /tmp/clip.webm       # VP9 WebM via ffmpeg
```

Without `-o`, artifacts land in `~/.chrome-ctl/media`. Screenshots and recordings are written with mode `0600`.

Google SSO is click-only: the extension finds the `accounts.google.com` popup, picks the requested account, and clicks Continue/Allow. It never types credentials; if a password prompt appears it stops and asks you to finish manually.

```bash
./bin/chrome-ctl sso --account me@example.com   # default timeout 15s
```

Macros are recorded by the daemon from successful CLI actions, so replay stays local and consumes no model tokens. Each recorded element keeps a multi-tier selector (test id, ARIA name, text, XPath, CSS) and replay tries them in order.

```bash
./bin/chrome-ctl macro start lead_search
./bin/chrome-ctl open "https://example.com/directory"
./bin/chrome-ctl type 1 "Software Agencies" --enter
./bin/chrome-ctl macro save --params search_term --description "Directory search"

./bin/chrome-ctl macro list
./bin/chrome-ctl macro run lead_search --vars '{"search_term":"Design Studios"}'
```

Macros are stored as `~/.chrome-ctl/macros/<name>.json` (mode `0600`). If a step cannot resolve its element, replay stops with `MACRO STEP FAILED` plus a fresh snapshot on stderr and exit code 1, so the agent can repair the step.

To verify trusted events locally, serve the bundled form fixture and automate it:

```bash
python3 -m http.server 8080 --directory tests/fixtures &
./bin/chrome-ctl open "http://localhost:8080/test_form.html"
./bin/chrome-ctl snap
./bin/chrome-ctl click <id>              # the page panel reports click trusted: true
./bin/chrome-ctl type <id> "Hello"       # and key trusted: true
```

After rebuilding the extension, click **Reload** on `chrome://extensions`. Content scripts are injected on demand into tabs that were already open, so no page reload is required.

- Daemon control: `chrome-ctl daemon --start|--stop`; background daemon logs go to `~/.chrome-ctl/daemon.log`.
- Auth token: `~/.chrome-ctl/auth_token` (mode `0600`); use `cat ~/.chrome-ctl/auth_token` to retrieve it later.

Automated checks:

```bash
go test ./...
(cd extension && npm run typecheck && npm run build && npm test)
bash tests/e2e/run.sh
```

The CLI provides `status`, `daemon`, `version`, `tabs`, `open`, `switch`, `snap`, `read`, `click`, `type`, `press`, `scroll`, `upload`, `wait`, `shot`, `rec`, `sso`, and `macro`.

---

## Packaging & agent skill

The `Makefile` wraps the build, test, and release flows:

```bash
make build      # bin/chrome-ctl, built with CGO_ENABLED=0
make extension  # install deps if needed, typecheck, build extension/dist
make test       # go test ./... plus extension typecheck/build/tests
make e2e        # build the CLI, then run tests/e2e/run.sh
make dist       # release layout in dist/
```

`make dist` cross-compiles `dist/chrome-ctl-darwin-arm64`, `dist/chrome-ctl-darwin-amd64`, and `dist/chrome-ctl-linux-amd64`, and stages the extension at `dist/extension` and the skill at `dist/SKILL.md`. `make clean` removes `bin/` and `dist/` only, leaving `extension/dist` and `extension/node_modules` untouched.

Install the agent skill by copying it into your agent's skills directory:

```bash
mkdir -p ~/.claude/skills/chrome-ctl
cp SKILL.md ~/.claude/skills/chrome-ctl/SKILL.md

# or, for Hermes
mkdir -p ~/.hermes/skills/chrome-ctl
cp SKILL.md ~/.hermes/skills/chrome-ctl/SKILL.md
```

WebP screenshots and storyboard recordings have no external dependencies. The build uses `-tags nodynamic`, which selects the pure-Go WebP backend instead of dlopen-ing a system `libwebp`; combined with `CGO_ENABLED=0` this yields statically linked binaries with no cgo or system library requirements. ffmpeg is an optional runtime dependency used only for WebM recording export; if it is missing, use `rec stop --grid` instead.

---

## 📄 License

MIT License.
