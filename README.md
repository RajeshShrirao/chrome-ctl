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

## Local Development (Phase 1)

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

- Daemon control: `chrome-ctl daemon --start|--stop`; background daemon logs go to `~/.chrome-ctl/daemon.log`.
- Auth token: `~/.chrome-ctl/auth_token` (mode `0600`); use `cat ~/.chrome-ctl/auth_token` to retrieve it later.

Automated checks:

```bash
go test ./...
(cd extension && npm run typecheck && npm run build)
bash tests/e2e/run.sh
```

Phase 1 provides `status`, `daemon`, and `version`; tab control, `snap`, and actions arrive in later phases.

---

## 📄 License

MIT License.
