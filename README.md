<p align="center">
  <img src="docs/assets/icon.png" width="128" height="128" alt="chrome-ctl icon" />
</p>

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

- **Small observations.** `snap` returns only interactive elements with numeric IDs that stay unique for the life of the document, typically a few hundred tokens instead of a full DOM dump.
- **Real sessions.** Google SSO, corporate portals, and social sites work because the browser is already authenticated.
- **Trusted input.** Clicks, typing, and scrolling use CDP input events (`event.isTrusted === true`) with human-like timing and paths.
- **Out of your way.** `chrome-ctl window ensure` puts the agent in its own unfocused Chrome window. Nothing steals your focus, your tabs stay where they are, and your video keeps playing.
- **Visual fallback.** Set-of-Mark WebP screenshots reuse snapshot IDs, so a vision model can act on what it sees.
- **Honest results.** Snapshot IDs never repeat inside a document, a dispatch timeout says the state is unknown instead of "not connected", and `type` reads the field back before pressing Enter.
- **SPA-aware waits.** `wait --network` observes in-flight requests through CDP, and `wait --for-text` waits for rendered content instead of DOM silence.
- **Structural reads.** `read --tables`, `read --links`, and `net --errors` return data an agent can use without a vision model.
- **Bulk extraction.** `bulk` scrapes a URL list into files and a manifest, keeping page bodies out of the conversation.
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

Put the binary for your platform on `PATH`, then load `extension/` the same way:

```bash
mkdir -p ~/.local/bin
cp dist/chrome-ctl-darwin-arm64 ~/.local/bin/chrome-ctl
chmod 0755 ~/.local/bin/chrome-ctl
export PATH="$HOME/.local/bin:$PATH"   # add to your shell profile to persist it

# Or system-wide, if you have admin rights:
sudo install -m 0755 dist/chrome-ctl-darwin-arm64 /usr/local/bin/chrome-ctl
```

Without either step, invoke it by path (`./bin/chrome-ctl`, or `dist/chrome-ctl-darwin-arm64`) — the skill's examples assume a bare `chrome-ctl`.

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
- The id survives service-worker suspension and browser restarts. If the window is gone, the stale id is dropped and the next `window ensure` creates a fresh one.
- After reloading the extension, re-run `window ensure` and confirm `window list` marks a window `(automation)`; until a live window is tracked, commands fall back to your focused window.
- Keep the window un-minimized. Chrome can only capture a visible tab, so a minimized automation window breaks `shot` and `rec`.
- Driving the user's window is still available and explicit: `window use focused`, then `window clear` when done.

### Observe

| Command | Description |
| :--- | :--- |
| `chrome-ctl snap` | Interactive elements in the viewport, one line each. |
| `chrome-ctl snap --all` | Include off-screen elements. |
| `chrome-ctl snap --forms` | Form controls and submit-like buttons only. |
| `chrome-ctl snap --deep` | Add rows, grid cells, and cursor-only controls (opt-in; capped). |
| `chrome-ctl snap --max-elements <n>` | Raise or lower the 400-element cap. |
| `chrome-ctl url` | Print the active tab URL in one line. |
| `chrome-ctl read [<id_or_selector>]` | Readable Markdown for the page or one element. |
| `chrome-ctl read --tables` | Tables as TSV with header rows. |
| `chrome-ctl read --links` | Visible links as `text<TAB>href`. |
| `chrome-ctl read --json` | Structured read result. |
| `chrome-ctl read --max-tables <n> --max-links <n>` | Caps for structured reads (defaults `10` and `200`). |
| `chrome-ctl describe <id\|css=selector>` | Tag, role, label, value, state, href, attributes, and geometry. |
| `chrome-ctl href <id>` | Only the resolved absolute URL. |
| `chrome-ctl net` | Requests seen while the debugger was attached. |
| `chrome-ctl net --errors --since 5m` | Failed request history. |
| `chrome-ctl net --json --limit <n>` | Raw JSON instead of TSV (default limit `50`). |

Snapshots look like this:

```text
SNAP 3 | TAB [7] "Sign in - Example" https://example.com/login
[1] <a> "Home" @12,80
[2] <input:text> "Search" @40,120
[3] <button> "Sign in" @40,160 collapsed
```

IDs are globally unique for the life of the document and are never reused, so an old ID either resolves to the same element or reports `not found`; it cannot silently become a different control. A navigation or reload creates a new document, and any action using pre-navigation IDs fails with `snapshot stale` before it runs. Re-snap after navigation or any layout-changing action.

### Act

| Command | Description |
| :--- | :--- |
| `chrome-ctl click <id>` | Humanized click. Flags: `--double`, `--snap`. Reports the element it landed on and whether the URL changed. |
| `chrome-ctl clickat <x>,<y>` | Click an exact viewport point in CSS pixels (from `snap`/`describe`). Flags: `--double`, `--snap`. |
| `chrome-ctl type <id> "text"` | Typing with cadence, optional typos, and a read-back check. Flags: `--enter`, `--snap`, `--typo-rate` (default `0.02`). |
| `chrome-ctl press <key>` | Press a key or combination. Repeat `--modifier ctrl\|alt\|shift\|meta`. |
| `chrome-ctl scroll <down\|up> [amount]` | Scroll in viewport fractions (default `0.8`). |
| `chrome-ctl upload <id> <file>` | Attach a local file to a file input. |
| `chrome-ctl wait <ms\|range\|idle\|selector>` | Pause, wait for DOM quiet, or wait for a selector. |
| `chrome-ctl wait --network` | Wait until no requests are in flight and none start for `--quiet-ms` (default 500). |
| `chrome-ctl wait --for-text "..."` | Wait until the rendered body contains the text. Add `--ignore-case`. |
| `chrome-ctl wait ... --timeout <ms>` | Deadline for selector, text, and network waits (default `15000`). |
| `chrome-ctl eval '<js>'` | Run JavaScript through CDP. Flags: `--raw`, `--await`, `--timeout`. |

`type` prints `(verified)` when the field reads back what was sent, and a `WARN` line when the field reads empty or something different. Exit code stays `0` in both cases: the input was dispatched, but the site may have rejected it. A `DISPATCHED, ... State unknown` error means the request was written without an acknowledgement — snap before retrying, and never blindly repeat a submit.

`clickat` takes viewport CSS pixels exactly as `snap` and `describe` print them. Screenshots are resized by `--max-width`, so do not convert image pixels to coordinates.

Actions on a tab attach `chrome.debugger`, so Chrome shows the yellow "started debugging this browser" infobar. That is expected. The attachment stays open so subsequent actions are fast and enables the network log; close DevTools on that tab if it is open, because it competes for the debugger.

`eval` is enabled by default and runs in the page through the debugger, so page CSP cannot block it. It is audit-logged and can read anything the page can read. Start the daemon with `chrome-ctl daemon --start --no-eval` to disable it; the gate is preserved across the background restart.

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

If `captureVisibleTab` cannot capture the tab (the window is minimized, or Chrome refuses a background capture), `shot` falls back to the debugger's `Page.captureScreenshot` and appends `via CDP fallback (captureVisibleTab failed)`. When both paths fail, the error names both causes. On canvas- or WebGL-heavy pages a screenshot is evidence of pixels, not proof of the underlying data — use `net` or `eval` for values.

### Bulk extraction

```bash
./bin/chrome-ctl bulk urls.txt --out ./pages --wait 6000-9000
./bin/chrome-ctl bulk queries.txt --template 'https://www.bing.com/search?q={{item}}' --out ./serp
./bin/chrome-ctl bulk - --out ./pages --resume < urls.txt
```

Each item is opened in the automation tab, given a jittered wait, checked with `wait --network` (falling back to DOM idle), read as markdown, and written atomically to `NNN-slug.md`. Transient failures are retried with full-jitter backoff, per-host pacing halves after a failure, and a host that keeps failing is paused by a circuit breaker. Every attempt appends `{index, url, title, bytes, file, waitedMs, ok, error, hash, simHash}` to `manifest.jsonl`. `--resume` skips successful URLs, `--stop-on-empty` stops on pages shorter than `--min-bytes` (default 300), and three consecutive near-duplicate pages (SimHash, not just an exact match) stop the run. stdout carries only the summary and failures; page bodies stay in files.

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
- `eval` runs page JavaScript through the debugger, bypasses page CSP, and is audit-logged. Disable it with `chrome-ctl daemon --start --no-eval` when it is not needed.
- The network log records request URLs and statuses for tabs with an attached debugger. It is capped at 200 entries per tab and stored in `chrome.storage.session`.
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
make extension    # typecheck, bundle, and verify extension/dist
make test         # Go tests, extension typecheck/build, extension tests
make e2e          # fake-extension harness, no Chrome required
make dist         # cross-compiled binaries, extension, SKILL.md
make clean        # remove bin/ and dist/
```

`make dist` is the production build. It typechecks and bundles the extension first, then writes `dist/chrome-ctl-darwin-arm64`, `dist/chrome-ctl-darwin-amd64`, `dist/chrome-ctl-linux-amd64`, `dist/extension/`, and `dist/SKILL.md`. Every binary is built with `CGO_ENABLED=0`, `-tags nodynamic`, and `-trimpath`.

The extension icons live in `extension/icons` as `icon16.png`, `icon32.png`, `icon48.png`, and `icon128.png`. The build copies them into `extension/dist/icons`, and `extension/scripts/verify.mjs` fails the build if the manifest references a missing size. The README artwork is `docs/assets/icon.png`. Replace the four sizes (and the 512 px copy) if the branding changes.

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

`SKILL.md` teaches an agent the task lifecycle — plan, operate, verify, recover, stop and ask, resume — plus the command reference behind it. Install it into your agent's skills directory:

```bash
# Qoder
mkdir -p ~/.qoder/skills/chrome-ctl
cp SKILL.md ~/.qoder/skills/chrome-ctl/SKILL.md

# Claude
mkdir -p ~/.claude/skills/chrome-ctl
cp SKILL.md ~/.claude/skills/chrome-ctl/SKILL.md

# Hermes
mkdir -p ~/.hermes/skills/chrome-ctl
cp SKILL.md ~/.hermes/skills/chrome-ctl/SKILL.md
```

`make dist` also writes `dist/SKILL.md`, the copy that ships with a release; the repo root is the single source and the two must stay byte-identical.

After rebuilding the extension, click **Reload** on `chrome://extensions`. Content scripts are injected on demand into already-open tabs, so pages do not need a refresh. Run `chrome-ctl window ensure` after a reload and check `window list` for the `(automation)` marker, so the next command does not resolve to your focused window.

## Troubleshooting

| Symptom | Likely cause and fix |
| :--- | :--- |
| `Daemon: STOPPED` | Run `chrome-ctl daemon --start`; inspect `~/.chrome-ctl/daemon.log`. |
| `Chrome Extension: DISCONNECTED` | Reload the extension; it re-pairs on reconnect. Click **Reconnect** in the popup to retry immediately. |
| Popup shows `Waiting for daemon` | Start the daemon; the popup pairs automatically within a few seconds. |
| Pairing keeps failing | Confirm the daemon is on `127.0.0.1:9229`, then use **Advanced → Manual token** with the output of `chrome-ctl token`. |
| Commands fail with `element not found` | Take a fresh `snap`; the page changed or the element is off-screen (`snap --all`). |
| `snapshot stale: page changed` | A navigation or reload replaced the document. Run `snap` and use the new IDs. |
| `DISPATCHED, ... State unknown` | The request was written but no ack arrived; the action may have happened. `snap`/`url` before retrying, and never repeat a submit blindly. |
| `type` warns `reads empty` | The site rejected or reverted the value (readonly, controlled input). Snap, click the field, and retry. |
| `feature disabled` from `eval` | The daemon runs with `--no-eval`. Restart it without that flag if page scripts are needed. |
| `wait --network` cannot enable network events | DevTools owns the debugger on that tab. Close DevTools or use `wait --for-text`. |
| `net` is empty | The log only fills while a debugger is attached. Run an action or `wait --network` first. |
| Snapshot misses a visible control | Try `snap --deep` or `snap --max-elements 800`; then `describe "css=..."`, keyboard navigation, or `clickat`. |
| Commands act on the wrong window | `chrome-ctl window list` shows which window is flagged `(automation)`; use `window use <id>`, `window ensure`, or `window clear`. After reloading the extension, run `window ensure` first. |
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
- Downloads cannot be verified: a download click is dispatched once, and confirming the file is left to the user.
- `wait --network` depends on CDP events and cannot observe requests made before the debugger attached. Long-polling pages may never settle; use `wait --for-text`.
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
