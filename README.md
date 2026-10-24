# chrome-ctl

**Drive your real, already-signed-in Chrome from the command line.**

`chrome-ctl` is a local daemon, CLI, and Manifest V3 extension that let an agent or a shell script control the browser profile you actually use. It reads compact element maps instead of raw HTML, dispatches trusted input through Chrome's debugging protocol, and keeps screenshots, recordings, and macros on your machine.

Because it runs inside your normal Chrome profile, every site sees your existing cookies, sessions, extensions, and fingerprint—no separate automation browser and no fresh login.

- [Quick start](#quick-start)
- [Commands](#commands)
- [How it works](#how-it-works)
- [Security model](#security-model)
- [Configuration](#configuration)
- [Development](#development)
- [Troubleshooting](#troubleshooting)
- [Documentation](#documentation)

## Why it exists

- **Small observations.** `snap` returns only interactive elements with stable numeric IDs, typically a few hundred tokens instead of a full DOM dump.
- **Real sessions.** Google SSO, corporate portals, and social sites work because the browser is already authenticated.
- **Trusted input.** Clicks, typing, and scrolling use CDP input events (`event.isTrusted === true`) with human-like timing and paths.
- **Out of your way.** `chrome-ctl window ensure` puts the agent in its own unfocused Chrome window. Nothing steals your focus, your tabs stay where they are, and your video keeps playing.
- **Visual fallback.** Set-of-Mark WebP screenshots reuse snapshot IDs, so a vision model can act on what it sees.
- **Repeatable workflows.** Record a successful run once, parameterize it, and replay it locally with no model calls.

No tool can guarantee that a site will not detect automation. `chrome-ctl` reduces obvious signals and respects site protections; it does not bypass them.

## Requirements

| Requirement | Notes |
| :--- | :--- |
| macOS or Linux | Release binaries target `darwin-arm64`, `darwin-amd64`, and `linux-amd64`. |
| Google Chrome 116+ | The extension uses Manifest V3 offscreen documents and `chrome.debugger`. |
| Go 1.23+ | Only needed to build from source. |
| Node.js 22+ | Only needed to build or test the extension. |
| `ffmpeg` (optional) | Required only for WebM export. Storyboard export needs no external tools. |

## Quick start

Build the CLI and extension from a checkout:

```bash
git clone https://github.com/RajeshShrirao/chrome-ctl.git
cd chrome-ctl

make build        # bin/chrome-ctl
make extension    # extension/dist (load-unpacked target)
```

Start the daemon. On first run it creates the auth token with mode `0600`:

```bash
./bin/chrome-ctl daemon --start
# Daemon: STARTED (pid 12345, 127.0.0.1:9229)
# Token file: ~/.chrome-ctl/auth_token
# Chrome extension: pairs automatically
```

Load the extension:

1. Open `chrome://extensions` and enable **Developer mode**.
2. Choose **Load unpacked** and select the `extension/dist` directory.
3. Open the **chrome-ctl Bridge** popup. It finds the daemon and pairs itself.
4. The popup should report **Connected**.

No tokens to copy. If the daemon is not running yet, the popup shows **Waiting for daemon** and pairs within a few seconds of it starting.

For a manual fallback, `./bin/chrome-ctl token` prints the token, and the popup's **Advanced → Manual token** section accepts it.

Verify the whole path:

```bash
./bin/chrome-ctl status
```

```text
Daemon: RUNNING (127.0.0.1:9229)
Chrome Extension: CONNECTED
Active Tabs: 3
```

`status` exits `1` when the daemon is unreachable, and `0` when the daemon is up even if the extension is disconnected.

Optional but recommended for agents: `./bin/chrome-ctl window ensure` opens a separate, unfocused window so automation never touches the tabs you are using.

### Install from a release build

`make dist` produces:

```text
dist/
├── chrome-ctl-darwin-arm64
├── chrome-ctl-darwin-amd64
├── chrome-ctl-linux-amd64
├── SKILL.md
└── extension/          # load this directory unpacked
```

Copy the binary for your platform onto `PATH`, then load `extension/` the same way.

## Commands

Every browser command needs a running daemon and a connected extension.

### Session and tabs

| Command | Description |
| :--- | :--- |
| `chrome-ctl status` | Daemon health, extension connection, and active tab count. |
| `chrome-ctl daemon --start` | Start the daemon in the background. |
| `chrome-ctl daemon --stop` | Stop the daemon and remove its PID file. |
| `chrome-ctl daemon` | Run the daemon in the foreground (logs to stderr). |
| `chrome-ctl token` | Print the auth token from `~/.chrome-ctl/auth_token` for manual pairing. |
| `chrome-ctl version` | Print the CLI version. |
| `chrome-ctl tabs` | List open tabs with their IDs. |
| `chrome-ctl open <url>` | Navigate the active tab. Add `--new` to open a new tab. |
| `chrome-ctl switch <tab_id>` | Make a tab active; the automation window follows that tab. |
| `chrome-ctl tabs --window <id>` / `open --window <id>` | Target one window explicitly instead of the automation window. |

### Windows

| Command | Description |
| :--- | :--- |
| `chrome-ctl window ensure` | Reuse the background automation window, or create an unfocused one. |
| `chrome-ctl window list` | Every open window, which is focused, and which is the automation window. |
| `chrome-ctl window new [url]` | Always create a fresh automation window. `--foreground` focuses it. |
| `chrome-ctl window use <id\|focused>` | Point automation at an existing window (the user's, by id or `focused`). |
| `chrome-ctl window clear` | Stop targeting a dedicated window; commands use the focused window again. |
| `chrome-ctl window close <id>` | Close a window. Refuses a focused one unless `--force`. |

### Background automation window

By default the agent works in a dedicated Chrome window it owns:

```bash
./bin/chrome-ctl window ensure          # unfocused window, created once and reused
./bin/chrome-ctl open "https://news.ycombinator.com"   # loads in that window
./bin/chrome-ctl snap                    # reads that window's active tab
```

- `window ensure` opens the window without taking focus, and the extension remembers its id in `chrome.storage.local`. Every later command that resolves "the active tab" prefers that window, so your own windows, tabs and video playback are never touched.
- Focus is only ever changed on request. `switch` activates a tab inside its own window, and screenshots use `captureVisibleTab` on that tab — neither raises the window. `window new --foreground` and `window use focused` are the deliberate exceptions.
- The yellow "…is debugging this browser" infobar from `chrome.debugger` appears on tabs in the automation window, not on the tabs you are using.
- The tracked id survives service-worker suspension, extension reload, and browser restart. A stale id (you closed the window) is dropped and the next `window ensure` creates a fresh one.
- Keep the window un-minimized. Chrome can only capture a visible tab, so a minimized automation window breaks `shot` and `rec`.
- Driving the user's window is still available and explicit: `window use focused`, then `window clear` when done.

### Observe

| Command | Description |
| :--- | :--- |
| `chrome-ctl snap` | Interactive elements in the viewport, one line each. |
| `chrome-ctl snap --all` | Include off-screen elements. |
| `chrome-ctl snap --forms` | Form controls and submit-like buttons only. |
| `chrome-ctl read [<id_or_selector>]` | Readable Markdown for the page or one element. |

Snapshots look like this:

```text
[1] <a> "Home" @12,80
[2] <input:text> "Search" @40,120
[3] <button> "Sign in" @40,160
```

IDs are assigned in document order for each snapshot. Treat a snapshot as the current map: after the page changes, take a new one.

### Act

| Command | Description |
| :--- | :--- |
| `chrome-ctl click <id>` | Humanized click. Flags: `--double`, `--snap`. |
| `chrome-ctl type <id> "text"` | Typing with cadence and optional typos. Flags: `--enter`, `--snap`, `--typo-rate` (default `0.02`). |
| `chrome-ctl press <key>` | Press a key or combination. Repeat `--modifier ctrl\|alt\|shift\|meta`. |
| `chrome-ctl scroll <down\|up> [amount]` | Scroll in viewport fractions (default `0.8`). |
| `chrome-ctl upload <id> <file>` | Attach a local file to a file input. |
| `chrome-ctl wait <ms\|range\|idle\|selector>` | Pause, wait for DOM quiet, or wait for a selector. |

Actions on a tab attach `chrome.debugger`, so Chrome shows the yellow "started debugging this browser" infobar. That is expected. The attachment stays open so subsequent actions are fast; close DevTools on that tab if it is open.

### Capture

| Command | Description |
| :--- | :--- |
| `chrome-ctl shot` | WebP screenshot of the active tab. |
| `chrome-ctl shot --som` | Overlay Set-of-Mark badges matching `snap` IDs. |
| `chrome-ctl shot --gray` | Grayscale output for text inspection. |
| `chrome-ctl shot -o <path>` | Choose the output file. |
| `chrome-ctl shot --max-width <px> --quality <1-100>` | Override defaults (`1024`, `65`). |
| `chrome-ctl rec start --fps 1\|2` | Start recording the active tab. |
| `chrome-ctl rec stop --grid -o <path>` | Export a 3×4 WebP contact sheet. |
| `chrome-ctl rec stop -o <path>.webm` | Export VP9 WebM (requires `ffmpeg`). |

Without `-o`, media is written to `~/.chrome-ctl/media` with mode `0600`.

### Auth and macros

| Command | Description |
| :--- | :--- |
| `chrome-ctl sso` | Find a Google OAuth popup and click the account picker. |
| `chrome-ctl sso --account <email>` | Pick a specific account instead of the first. |
| `chrome-ctl macro start <name>` | Start recording successful CLI actions. |
| `chrome-ctl macro save --params a,b --description "..."` | Save the recording with `{{a}}` / `{{b}}` templates. |
| `chrome-ctl macro list` | List saved macros. |
| `chrome-ctl macro run <name> --vars '{"a":"value"}'` | Replay locally. |

## A typical loop

A compact read-act-verify cycle:

```bash
./bin/chrome-ctl window ensure           # the agent's own unfocused window
./bin/chrome-ctl open "https://news.ycombinator.com"
./bin/chrome-ctl snap
# [1] <a> "new" @20,48
# [2] <a> "past" @60,48
./bin/chrome-ctl click 1 --snap        # click and return the next map
./bin/chrome-ctl read 3                # Markdown for one element
./bin/chrome-ctl click 5               # act on the new snapshot
```

Use `snap` while the UI is textual; reach for `shot --som` when icons, canvas, or layout make text ambiguous.

### Filling a form

```bash
./bin/chrome-ctl snap --forms
./bin/chrome-ctl type 4 "Autonomous AI Agent"
./bin/chrome-ctl upload 5 ./logo.png
./bin/chrome-ctl click 6 --snap
```

### Recording a reusable macro

```bash
./bin/chrome-ctl macro start lead_search
./bin/chrome-ctl open "https://example.com/directory"
./bin/chrome-ctl type 1 "Software Agencies" --enter
./bin/chrome-ctl macro save --params search_term --description "Directory search"

./bin/chrome-ctl macro run lead_search --vars '{"search_term":"Design Studios"}'
```

The daemon records the element descriptor before each action mutates the page, so navigation does not erase the target. Replay resolves test IDs, ARIA names, text, XPath, and CSS in order. If a step cannot resolve, replay stops with `MACRO STEP FAILED`, a fresh snapshot, and exit code `1`.

Macros live in `~/.chrome-ctl/macros/<name>.json` with mode `0600`.

### Google SSO

```bash
./bin/chrome-ctl sso --account me@example.com
```

The extension finds the `accounts.google.com` popup, selects the account, and clicks Continue or Allow. It never types credentials. If a password or CAPTCHA appears, it stops and leaves the step to you.

## How it works

```text
Agent or shell
      │
      ▼
chrome-ctl CLI ─── HTTP POST /rpc (Bearer token) ───▶ daemon (127.0.0.1:9229)
                                                         │
                                                         │ WebSocket /ws (token)
                                                         ▼
                                                  MV3 extension
                                    ┌──────────────────┴──────────────────┐
                              offscreen document                  service worker
                              (persistent socket)              (tabs, debugger, SSO)
                                                                        │
                                                                 content scripts
                                                                        │
                                                                  Chrome tabs
```

- The **CLI** is stateless. It reads the token from disk and sends JSON-RPC 2.0 requests to the daemon.
- The **daemon** authenticates requests, keeps media and macro state, and forwards browser work.
- The **extension** keeps the WebSocket alive in an offscreen document, so Manifest V3 service-worker suspension does not drop the bridge. It also owns the automation-window id in `chrome.storage.local`, which is why the CLI stays stateless and the window survives a restart.
- **Content scripts** build snapshots, read Markdown, and describe elements across open shadow roots and same-origin iframes.

The wire protocol is specified in [docs/PROTOCOL.md](docs/PROTOCOL.md). Architecture and lifecycle details are in [docs/TECH_SPEC.md](docs/TECH_SPEC.md) and [docs/MV3_LIFECYCLE_SPEC.md](docs/MV3_LIFECYCLE_SPEC.md).

## Security model

`chrome-ctl` is intentionally local and single-user.

- The daemon binds only to `127.0.0.1`.
- Every HTTP and WebSocket request requires a comparison against a 64-character token stored at `~/.chrome-ctl/auth_token` (mode `0600`, directory `0700`).
- `POST /pair` is the only unauthenticated route. It requires a loopback peer **and** loopback `Host`, the custom `X-Chrome-Ctl-Pair: 1` header, and an `Origin` that is empty or `chrome-extension://…`. A web page cannot satisfy the header check cross-origin, so it cannot read the token.
- Tokens and request URLs are never written to daemon logs.
- The extension sends the token only to the loopback daemon and stores it in `chrome.storage.local`.
- SSO automation is click-only and aborts at password prompts; credentials and CAPTCHAs are left to you.
- Screenshots, recordings, and macros stay under `~/.chrome-ctl`.
- `chrome.debugger` grants broad control over the attached tab. Attach deliberately and stop the daemon when you are done.

Do not commit the token, paste it into a website, or share logs that contain it. To rotate it, stop the daemon, delete `~/.chrome-ctl/auth_token`, and start the daemon again; the extension re-pairs automatically. Any local process running as you can already read the `0600` token file, so pairing does not widen the trust boundary.

## Configuration

| Variable | Default | Purpose |
| :--- | :--- | :--- |
| `CHROME_CTL_HOME` | `~/.chrome-ctl` | Config, token, media, macro, and log directory. |
| `CHROME_CTL_ADDR` | `127.0.0.1:9229` | Daemon listen address. Intended for tests and isolated instances. |
| `CHROME_CTL_FFMPEG` | unset | Explicit `ffmpeg` path for WebM export. |

The extension connects to `ws://127.0.0.1:9229/ws`, so changing `CHROME_CTL_ADDR` requires a matching extension build. Use the default for normal operation.

Layout:

```text
~/.chrome-ctl/
├── auth_token      # 0600
├── daemon.pid
├── daemon.log      # background daemon stdout/stderr
├── media/          # screenshots and recordings
└── macros/         # <name>.json
```

## Development

```bash
make build        # static CLI with CGO_ENABLED=0 and -tags nodynamic
make extension    # typecheck and bundle extension/dist
make test         # Go tests, extension typecheck/build, extension tests
make e2e          # fake-extension harness, no Chrome required
make dist         # cross-compiled binaries, extension, SKILL.md
make clean        # remove bin/ and dist/
```

The `nodynamic` build tag selects the pure-Go WebP backend instead of dynamically loading a system `libwebp`. Together with `CGO_ENABLED=0` this produces statically linked binaries. If you build manually, include the tag:

```bash
go build -tags nodynamic -o bin/chrome-ctl ./cmd/chrome-ctl
go test -tags nodynamic ./...
```

Extension tests run under Node's test runner with jsdom:

```bash
cd extension
npm install
npm run typecheck
npm run build
npm test
```

The E2E harness starts a temporary daemon and a fake extension, then exercises authentication, forwarding, actions, media, SSO, and macros:

```bash
bash tests/e2e/run.sh
```

### Fixtures

`tests/fixtures` contains local pages for manual and automated checks: a form with trusted-event reporting, open shadow DOM, nested same-origin iframes, a modal dialog, and infinite scroll.

```bash
python3 -m http.server 8080 --directory tests/fixtures
./bin/chrome-ctl open "http://localhost:8080/test_form.html"
./bin/chrome-ctl snap
```

### Agent skill

`SKILL.md` teaches an agent the snapshot-first workflow, action limits, and macro reuse. Install it into your agent's skills directory:

```bash
mkdir -p ~/.claude/skills/chrome-ctl
cp SKILL.md ~/.claude/skills/chrome-ctl/SKILL.md

# Hermes
mkdir -p ~/.hermes/skills/chrome-ctl
cp SKILL.md ~/.hermes/skills/chrome-ctl/SKILL.md
```

After rebuilding the extension, click **Reload** on `chrome://extensions`. Content scripts are injected on demand into already-open tabs, so pages do not need a refresh.

## Troubleshooting

| Symptom | Likely cause and fix |
| :--- | :--- |
| `Daemon: STOPPED` | Run `chrome-ctl daemon --start`; inspect `~/.chrome-ctl/daemon.log`. |
| `Chrome Extension: DISCONNECTED` | Reload the extension; it re-pairs on reconnect. Click **Reconnect** in the popup to retry immediately. |
| Popup shows `Waiting for daemon` | Start the daemon; the popup pairs automatically within a few seconds. |
| Pairing keeps failing | Confirm the daemon is on `127.0.0.1:9229`, then use **Advanced → Manual token** with the output of `chrome-ctl token`. |
| Commands fail with `element not found` | Take a fresh `snap`; the page changed or the element is off-screen (`snap --all`). |
| Commands act on the wrong window | `chrome-ctl window list` shows which window is flagged `(automation)`; use `window use <id>` or `window clear`. |
| `shot`/`rec` fail on the automation window | The window is minimized. Chrome can only capture a visible tab; restore the window or run `window ensure` for a fresh one. |
| `window close` refuses | The target has focus, so it is the window you are using. Pass `--force` only if you meant it. |
| Debugger attach fails | Close DevTools on that tab, or stop another tool using `chrome.debugger`. |
| Screenshot fails with a WebP error | Rebuild with `make build` (missing `-tags nodynamic`). |
| WebM export fails | Install `ffmpeg`, set `CHROME_CTL_FFMPEG`, or use `rec stop --grid`. |
| Popup never connects | Confirm the daemon is on `127.0.0.1:9229`; the extension URL is fixed. |
| Content commands do nothing on an old tab | Reload the tab or navigate once so the content script is injected. |

Status semantics: `status` exits `1` only when the daemon cannot be reached. A reachable daemon with no extension still exits `0` and prints `DISCONNECTED`.

## Limitations

- macOS and Linux only; no Windows build target.
- The extension drives the active tab; screenshots and recordings capture the visible tab of a window, not background tabs. A minimized window cannot be captured.
- Sites that block automation, require interactive CAPTCHAs, or prohibit scripted access remain your responsibility.
- WebM export depends on an external `ffmpeg`; WebP artifacts do not.
- The debugger attachment is visible to Chrome and can conflict with DevTools or other debugging extensions.

## Documentation

| Document | Scope |
| :--- | :--- |
| [docs/PRD.md](docs/PRD.md) | Product requirements, command surface, and token budget. |
| [docs/TECH_SPEC.md](docs/TECH_SPEC.md) | Architecture, stack, security model, and layout. |
| [docs/PROTOCOL.md](docs/PROTOCOL.md) | JSON-RPC schemas for HTTP `/rpc` and WebSocket `/ws`. |
| [docs/MV3_LIFECYCLE_SPEC.md](docs/MV3_LIFECYCLE_SPEC.md) | Offscreen socket host, service-worker awakening, debugger lifecycle. |
| [docs/HUMANIZER_SPEC.md](docs/HUMANIZER_SPEC.md) | Mouse paths, typing cadence, and scroll behavior. |
| [docs/MEDIA_PIPELINE.md](docs/MEDIA_PIPELINE.md) | WebP compression, Set-of-Mark overlays, storyboards. |
| [docs/MACRO_SCHEMA.md](docs/MACRO_SCHEMA.md) | Macro file schema, selector tiers, and replay rules. |
| [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md) | Six-phase roadmap and verification checklist. |
| [SKILL.md](SKILL.md) | Agent-facing operating instructions. |

## License

MIT License.
