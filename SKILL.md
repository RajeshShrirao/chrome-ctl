---
name: chrome-ctl
description: Drive the user's authenticated Chrome from the CLI: plan, operate, verify each action against real output, recover from dispatch failures, stop for credentials and destructive steps, and resume interrupted work. Snapshots, SPA-aware waits, bulk extraction, and local macros run from an unfocused background window.
---

# Chrome Control (`chrome-ctl`)

`chrome-ctl` drives the user's real, already-signed-in Google Chrome through a local daemon and a Manifest V3 extension. Actions use trusted input, so sessions, cookies, and hardware fingerprints are inherited from the user's normal profile.

This file is organised as the work itself — **plan, operate, verify, recover, stop and ask, resume** — followed by a command reference. Read the lifecycle before touching a browser; use the reference for exact flags.

**Binary.** `make build` produces `./bin/chrome-ctl`; a release ships `dist/chrome-ctl-darwin-arm64` and friends. Bare `chrome-ctl` works only if the user installed it onto `PATH`. Examples write `chrome-ctl`; prefix them with your own path.

## Four rules that never bend

1. **Snapshot first, never HTML.** `snap` returns a compact map of interactive elements with numeric IDs (`[1]`, `[2]`). Never request raw HTML, never paste a DOM into the conversation.
2. **IDs die with the document.** A navigation, reload, or new tab resets them and they are never reused. Act only on IDs from the latest snapshot.
3. **Exit 0 is not proof.** It means the input was dispatched, not that the site accepted it. Verify against the output text — see [Verify](#verify).
4. **Secrets and humans are hand-back points.** Passwords, one-time codes, CAPTCHA, 2FA, passkeys, wallets, downloads, and anything destructive or spendable stop the run — see [Stop and ask](#stop-and-ask).

## Task lifecycle

Six phases. Name the one you are in when you narrate progress, and do not start the next until the current one has evidence.

| Phase | Done when |
| :--- | :--- |
| 1. Plan | Goal, terminal evidence, and execution vehicle written down; daemon `RUNNING`, extension `CONNECTED`, automation window yours, `macro list` checked. |
| 2. Operate | Each mutation is followed by its observation; no stale IDs used. |
| 3. Verify | The output line you predicted before acting is the one you got. |
| 4. Recover | Errors mapped to their next command, not retried blind. |
| 5. Stop and ask | Blocker captured with evidence and handed to the user, or nothing was blocked. |
| 6. Resume | Journal updated; a fresh session could continue from it. |

## Plan and preflight

Write one line before any browser command: the goal, and **what output proves it is done** — a file path, a URL, a visible string. That line becomes the check in [Verify](#verify) and the record in [Resume](#resume).

```bash
chrome-ctl status                       # daemon + extension, three lines
chrome-ctl daemon --start               # only if status exits 1
chrome-ctl window ensure                # reuse or create your unfocused window
chrome-ctl window list                  # * focused, (automation) yours
chrome-ctl macro list                   # a saved macro costs zero tokens
```

- `status` prints `Daemon: RUNNING|STOPPED (addr)`, `Chrome Extension: CONNECTED|DISCONNECTED`, `Active Tabs: N`. **Exit code 0 does not mean ready.** It exits 1 only when the daemon is unreachable — silently, with no `Error:` line — and exits **0 while printing `Chrome Extension: DISCONNECTED`**, in which case every browser command will fail. Read line 2.
- A disconnected extension is the user's action, not yours: ask them to load or reload `extension/dist` at `chrome://extensions`; it pairs with the daemon on its own, and `status` should then report `CONNECTED`.
- `window ensure` prints `OK: using automation window [1724518459] (tab [1724518460], 1 tab, unfocused)` when it reuses, or `OK: created automation window …` when it makes one. It is idempotent and never steals focus, so it is safe even while the user watches a video. Confirm `unfocused`, and confirm `window list` marks `*` somewhere else.
- `window list` marks `*` on whichever window has focus (none, if the user is in another app), `(automation)` on yours, and the window state — `(maximized)`, `(minimized)`, `(fullscreen)`. Confirm `window ensure` said `unfocused` and that `*` is not on your window.
- `daemon --start` reports `Daemon: STARTED (pid 12345, 127.0.0.1:9229)`, `ALREADY RUNNING`, `Token file: ~/.chrome-ctl/auth_token`, `Chrome extension: pairs automatically`.
- `~/.chrome-ctl` legitimately holds only `auth_token`, `daemon.pid`, `daemon.log`; `macros/` and `media/` appear on first use.

**Choose the vehicle.**

| Shape | Use | Why |
| :--- | :--- | :--- |
| A few pages, judgement needed | Ad-hoc commands | Output enters the conversation where you can read it. |
| Ad-hoc verified sequence (2-10 steps) | `batch "<cmd1>" "<cmd2>"` | Fast sequential execution; fails fast with fresh snapshot attached; no record/save ceremony. |
| A known list, no judgement per page | `bulk --out <dir>` | Bodies go to files; token cost stays flat; a ledger makes it resumable. |
| A verified sequence run more than once | `macro` | Replays locally from resilient selectors; zero model tokens. |

There is **no `--dry-run` anywhere** and **no `macro delete`** (`macro` has only `start|save|list|run`). Rehearse a risky flow against the local fixtures instead of a live site:

```bash
python3 -m http.server 8080 --directory tests/fixtures
chrome-ctl open "http://localhost:8080/test_form.html"
```

Retire a macro by re-saving the same name, or ask the user.

## Operate

One mutation, one observation. Never chain a second mutation onto an unverified first one.

```bash
chrome-ctl open "https://example.com"   # OK: navigated [81] Title - https://...
chrome-ctl url                          # cheap "where am I" — one line
chrome-ctl snap                         # the element map
chrome-ctl click "Submit"               # direct click by text — no prior snap needed!
chrome-ctl click 5                      # OK: clicked [5] <button> @... -> dialog opened (0.04s)
chrome-ctl click 5 --snap               # returns compact DIFF (+2 added) instead of 400 lines
chrome-ctl type 3 "Hello" --enter --snap
chrome-ctl wait --network               # let the app catch up
chrome-ctl read                         # only once the page is what you expect
```

- **Instant Ballistic Clicks (<60ms)**: By default, clicks execute via 1–2 point ballistic human movement and 20–35ms press/release. No artificial delays or laggy multi-second curves.
- **Direct 1-Shot Target Clicking**: You don't need to run `snap` before clicking! Run `click "Text"`, `click "css=#selector"`, or `click <id>`. If targeting by text or CSS, `chrome-ctl` resolves the target directly in the page, saving an entire conversation turn and ~1,500 tokens.
- **Zero-Token Consequence Receipts**: The click receipt automatically proves the immediate consequence inline (e.g. `-> selected (•)`, `-> unselected ( )`, `-> opened dialog "Delete"`, `-> navigated to ...`). Do NOT waste tokens running `snap` after a click if the consequence receipt already confirms your action worked.
- `--snap` after `click`/`type` emits a compact **differential snapshot** (`DIFF (+N added, ~M changed)`) showing only what mutated, saving ~90% of tokens compared to a full DOM dump.
- The snapshot header is `SNAP <n> | TAB [<tabId>] <title> | <url>`. A **new** `<n>` is the only proof a new observation happened; the counter is not part of the IDs. The tab id is your resumable locator.
- `open` prints `(still loading)` when the load was not complete at return — that is a cue to `wait`, not to `read`.
- After `window ensure`, every command without an explicit tab resolves to the active tab of the automation window. Do not navigate, switch, or close tabs or windows the user owns.
- `chrome-ctl macro list` before any multi-step or repeatable task, and `chrome-ctl shot --som` only when icons, canvas, or layout make text ambiguous — a screenshot costs far more than a snapshot.

## Verify

Exit codes: **only `0` and `1` exist.** A failure prints `Error: <text>` to stderr — except `status` with an unreachable daemon and a failed `macro run`, which exit 1 **silently**, with no `Error:` line. `macro run` puts its failure detail on stderr, so never pipe stdout looking for it.

Predict the proof line before the action, then match it:

| Command | Proof it worked | What it still does not prove |
| :--- | :--- | :--- |
| `type` | `OK: Typed 12 chars into [3] (verified; …).` | The site accepted the value on submit. |
| `type` | `OK: Typed 12 chars into [3] (…)` — **no `verified`** | Read-back did not confirm. Treat as unverified. |
| `type` | `WARN: Typed 12 chars into [3] but the field reads empty — the site may have rejected it (readonly/controlled). Snap, click the field, retry.` | Nothing. Exit code is still 0. |
| `type` | `WARN: … but the field reads "…" instead. Check the target before retrying.` | Nothing. Wrong target or autofill fought you. |
| `click` | `OK: clicked [41] <input> "CPU" @320,410 -> selected (•) (0.04s)` | Hit coordinates and immediate consequence. If consequence shows mutation (e.g. `selected (•)`, `opened dialog`), action is verified without a re-snapshot. |
| `wait` | `OK: network quiet after 3.2s (14 requests)`, `OK: text "…" appeared after 1.1s`, `OK: selector "…" appeared after …`, `OK: idle after …` | Any `WARN: … State unknown.` line is a **timeout**, never proof the page is static. |
| `open` | `OK: navigated [81] <title> - <url>` | That the URL is where you meant to land; check for a redirect. |
| `snap` | a `SNAP <n>` counter higher than last time | Nothing about the previous action; it only re-observes. |
| `bulk` | `OK: 12 scraped, 0 failed -> ./pages (manifest.jsonl)`, plus `FAIL 007 <url>: <reason>` lines | Exits **1** if any item failed; bodies are in files, not stdout. |
| `macro run` | `OK: ran macro "directory_search" (7 steps, 12.4s)` | That the site's end state is right — `snap` after. |
| `batch` | `[1/3] OK: clicked [5]... [2/3] OK: text "Saved" appeared...` | All commands succeeded in sequence. Fails fast with fresh snapshot on first failure. |
| `sso` | `OK: signed in as user@example.com (redirect complete)` | Read `url` to confirm you are back on the site. |
| `shot` | `OK: saved /tmp/view.webp (1024x768, 41 KB, 42 badges)` | Pixels are evidence of appearance, not of data. Use `net` or `eval` for values. |

- After a submit, prove the **consequence**: `url` for a navigation, `wait --for-text "<the success string you expect>"`, then `snap` or `read`. `OK: pressed Enter` proves a key was sent, nothing more.
- `press` → `OK: pressed <key>`, `scroll` → `OK: scrolled down 812px to 2436px (34%)`, `switch` → `OK: switched to tab [81]`. All three are dispatch receipts.
- Before a high-stakes Post, Send, or Submit, verify the draft with `snap` (or `shot --gray`), then take a confirmation snapshot after it.

## Recover

Match the literal text, then run the named command. Do not improvise a retry.

| What you see | Next move |
| :--- | :--- |
| `cannot read auth token from ~/.chrome-ctl/auth_token: …; start the daemon with \`chrome-ctl daemon --start\`` | `daemon --start`, then `status`. |
| `status` prints `Daemon: STOPPED`, or `Chrome extension not connected (daemon reported: …)` | Start the daemon; for the extension, hand back to the user to load/reload `extension/dist`, then require `CONNECTED`. |
| `DISPATCHED, …. State unknown — run \`chrome-ctl snap\` before retrying; do not repeat the action blindly` — the internal-timeout variant reads `DISPATCHED, the extension stopped waiting for an ack. …` | `snap` or `url` first. Effect visible → continue forward. Effect absent **and** it was a Submit, Send, delete, or purchase → [Stop and ask](#stop-and-ask). Never re-fire. |
| `snapshot stale: page changed since snapshot <n>; run snap` | `snap`, re-locate the element by name or text, use the new ID. |
| `element [<id>] not found in the current snapshot`, `element id <n> not found; run snap first`, `element not found (<target>)` | `snap` → `snap --all` → `snap --deep` → `describe "css=<selector>"` → `press Tab` to the control → `clickat X,Y`. |
| `element [<id>] is not interactable (<reason>)` | It is hidden or covered: `scroll`, dismiss the banner that intercepts clicks, or click the `aria-expanded` parent, then `snap`. |
| `selector "<css>" matched no elements`, `invalid selector "<css>"` | The selector is wrong or the element is not rendered yet: `snap --deep`, or scope from an ID with `describe <id>`. |
| `tab <id> not found` | `tabs` (or `tabs --window <id>`), then `switch <id>` or `window ensure`. |
| `window <id> not found`, `window <id> has no active tab`, `no focused window found` | `window list`, then `window ensure` or `window use <id>`. |
| `debugger <operation> failed: <reason>` | Retry once — navigation and DevTools open detach it. If it repeats, ask the user to close DevTools or stop any other tool using `chrome.debugger`. |
| `failed to navigate the active tab`, `invalid url: <value>`, `url is required` | Check `url`, then `open` again. |
| `<protocol> pages do not allow content scripts` | `chrome:`, `chrome-extension:`, `chrome-untrusted:`, `edge:`, `about:`, `devtools:`, and `view-source:` are unreachable — including `chrome://extensions` itself. Ask the user for anything there. |
| `unknown key: <key>` | `press` accepts `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`, `Space`, `Home`, `End`, `PageUp`, `PageDown`, `ArrowUp/Down/Left/Right`, or a single character. |
| `SSO timeout: no Google OAuth popup detected`, `SSO timeout: Google OAuth popup did not complete` | `snap`: did the popup close, become a consent screen, or need a human? |
| `SSO aborted: password required; complete sign-in manually` | Hand back. Never type a password. |
| `BATCH STEP FAILED: step <n> ("<cmd>"): <reason>` + fresh snapshot on stderr | Read the attached snapshot to see why the step failed. Do NOT retry blindly. Fix the step or continue manually. |
| `MACRO STEP FAILED: macro "<name>" step <n>: <reason>` + a fresh snapshot on stderr | Read that snapshot, repair the step with normal commands, then re-record and `macro save` under the same name. Never rerun the macro unchanged. |
| `feature disabled` from `eval` | The daemon runs with `--no-eval`. No workaround; ask before `daemon --stop && daemon --start`. |

Not errors, and not reasons to retry: `OMITTED: <n> candidates (use snap --deep / --max-elements to see more)`; a `net` note on stderr (`no network data yet: … collected only while the debugger is attached`) means nothing was observed, not that the page made no requests; `WARN: DOM idle wait timed out after …`.

If the daemon restarts, check `~/.chrome-ctl/daemon.log` before assuming the worst. If a command reads the wrong page, `window list` shows which window is `(automation)`; point at the right one with `window use <id>` instead of touching the user's window.

## Stop and ask

Trigger the hand-back when you **observe** any of these:

- A `type=password` field, or a password/passphrase/API-key/card-number/one-time-code input.
- `SSO aborted: password required; complete sign-in manually`, or a CAPTCHA, passkey, 2FA, or wallet screen.
- A control labelled Delete, Purchase, Pay, Post, Send, Grant access, submitting a graded assessment / test / quiz, or anything that spends money or is irreversible.
- A download control — this build cannot verify a download landed.
- Page data about to leave to a third party, or a task that would exfiltrate content beyond its scope.

Sequence — five steps, no improvisation:

1. **Freeze.** Issue no further mutating command. Reads only.
2. **Capture.** `snap` and `url`; add `shot --gray -o /tmp/blocked.webp` if the blocker is visual.
3. **Preserve.** Leave the tab exactly where the user can finish it. Do **not** `window clear`, `window close`, or navigate away.
4. **Ask once**, with: the one-line goal, current `url` and the tab/window IDs from the `SNAP` header, the literal evidence line, the single action you need from them, and "I will not proceed until you confirm."
5. **Re-`snap`** after they confirm — their manual work invalidated every ID you held.

The CLI has **no approval gate, no `--confirm`, and no pause**. Enforcement is behavioural, backed by two real guards: `sso` refuses to type credentials, and `--no-eval` removes script execution entirely. Click a download control at most once, then ask. Never invent, guess, or paste a secret, and never put one in a URL or query string.

## Resume

**Persists across an interruption:**

- The automation window id, held in the extension's local storage. It survives service-worker teardown, a daemon restart, and an extension reload. After a **Chrome restart** the stored id is stale; `window ensure` detects that, drops it, and creates a fresh window — which is why re-running it is always the correct first move.
- Macros in `~/.chrome-ctl/macros/<name>.json`, as resilient selectors (test id, ARIA name, text, XPath, CSS in that order), never snapshot IDs.
- `bulk` progress in `<out>/manifest.jsonl` — one JSON line per attempt with `index, url, title, bytes, file, waitedMs, ok, error` — and `NNN-slug.md` filenames whose index is stable across resumes.
- The network log mirrors to session storage, so `net --since 5m` survives service-worker suspension but **not** a Chrome quit.
- `~/.chrome-ctl/media`, and `daemon.log`.

**Does not persist:** snapshot IDs and the `SNAP` counter across navigation, reload, or a new tab; tab and window numbers across a Chrome restart; any page state; and **the task itself** — `chrome-ctl` keeps no session, checkpoint, or history for ad-hoc work.

**Journal convention** (a documentation habit, not a CLI feature — no `chrome-ctl` command reads or writes this file):

```bash
JOURNAL="${CHROME_CTL_HOME:-$HOME/.chrome-ctl}/tasks/$(date +%Y%m%d)-signup.journal.jsonl"
mkdir -p "$(dirname "$JOURNAL")"
```

Append one JSON object per step: `{"goal":…,"url":…,"snap":"SNAP 4 | TAB [81] …","evidence":"Landed on button \\"Connect\\"","next":"type message","blocked":null}`. Use `CHROME_CTL_HOME` rather than `~/.chrome-ctl` so an alternate or containerised state directory keeps working; the directory is mode `0700`.

Recipes:

- **New agent session, work already started.** `status` → require `CONNECTED` → `window ensure` → `window list` → `url` → `snap`. Re-derive your position from the URL and the fresh snapshot; never reuse an ID from the previous session. Read the journal last line for intent.
- **Interrupted `bulk`.** Re-run the **same input list and the same `--out`** with `--resume`; already-`ok` URLs are skipped. Read `[stopped: …]` from the previous summary first — a short page or three identical pages means a rate limit or login wall, and resuming without fixing it just scrapes garbage.
- **Broken macro.** Follow the `MACRO STEP FAILED` row in [Recover](#recover), then re-save under the same name.
- **Chrome restarted.** `window ensure` replaces the stale window id; the pages inside it are gone, so re-`open` from the journal's last URL. Check `window list` for `(minimized)` before any `shot` or `rec stop`.
- **Daemon died.** `daemon --start` → `status` must show `CONNECTED` (the extension re-pairs unaided) → `window ensure` → `snap`.

# Reference

## Windows and focus

```bash
chrome-ctl window ensure                    # reuse or create the unfocused window
chrome-ctl window list                      # * focused, (automation) yours, (minimized) etc.
chrome-ctl window new "https://example.com" # always a fresh window
chrome-ctl window use <id|focused>          # point the whole workspace elsewhere
chrome-ctl window clear                     # stop targeting a dedicated window
chrome-ctl window close 12                  # tidy up a window you created
```

- Never use `window new --foreground`, `window use focused`, or `window close --force` unless the user explicitly asked you to work in their window — then say which window you took, and `window clear` when done.
- `window close` refuses a focused window because that is the one the user is looking at (`window <id> is focused; pass --force …`). `switch <tab_id>` also moves the workspace to that tab's window, so implicit commands follow it.
- Keep the automation window visible, not minimized: Chrome can only capture a visible tab.

## Snapshots, reads, waits

```bash
chrome-ctl snap                             # interactive elements in view
chrome-ctl snap --frames                    # traverse all frames regardless of size/visibility
chrome-ctl snap --forms                     # inputs, selects, textareas, submits
chrome-ctl snap --deep                      # cursor:pointer controls, bare rows/cells
chrome-ctl snap --all                       # include off-screen elements
chrome-ctl snap --max-elements 800          # raise the 400 cap
chrome-ctl read                             # page markdown (the default for prose)
chrome-ctl read --forms                     # markdown with (•)/( ), [x]/[ ] form states
chrome-ctl read --tables                    # "# table 1: caption" then TSV
chrome-ctl read --links                     # text<TAB>absolute href, deduped
chrome-ctl read --json                      # structured, no markdown mixed in
chrome-ctl read 12 --tables                 # scope to an element or snapshot ID
chrome-ctl formstate                        # every control across frames with frame paths
chrome-ctl formstate --frame f2             # filter controls inside frame f2
chrome-ctl scroll --to "div#question5"      # jump directly to element
chrome-ctl net --errors --since 5m --limit 50
```

- `--max-links` (200) and `--max-tables` (10) cap the two read modes.
- `net` only knows what happened while the debugger was attached — roughly during actions and `wait --network`. An empty log with a note means nothing was observed, not that the page made no requests. If DevTools owns the tab, the log cannot be collected.
- Waits: `wait 2500`, `wait 3000-7000` (jitter), `wait "#results"` (selector), `wait idle` (DOM quiet + document complete, **not** network), `wait --network` (nothing in flight and no new request for `--quiet-ms`, default 500), `wait --for-text "…"` with `--ignore-case`, and `--timeout` in ms (default 15000). Combine `--network --for-text` under one deadline.
- SPAs render a shell, go quiet, then fetch for seconds more. `wait idle` alone routinely returns too early — use `--network` or `--for-text` for fetched content. Long-polling, WebSockets, and streams may never settle: prefer `--for-text` with an explicit `--timeout`.
- `--network` needs the debugger; if DevTools is open on the tab, it reports that network events could not be enabled rather than pretending the page was quiet. If `wait --network` reports quiet after 0ms (0 requests observed), this indicates no network activity was observed, not proof of quiet — a `WARN` is emitted. Verify state.

## When the snapshot can't see it

- `collapsed`/`expanded` come from `aria-expanded` or a `<details>` summary. A closed combobox is visible; its options are not. Click it, then `snap`.
- Hidden file inputs are emitted at their visible `<label>` with state `file`. Use `upload <id> <path>`; never click the hidden input.
- Keyboard fallback reaches what pointer heuristics miss: `press Tab` until the control is focused, then `press Enter` or `press Space`.
- **Frames**: If a page looks nearly empty, assume a frame. Same-origin frames are readable by `read`; snap/click traversal may miss them if zero-sized or deeply nested. Use `snap --frames` to force-traverse all same-origin frames. Elements inside frames are tagged with their frame (e.g. `[41] (f2)`). Cross-origin iframes and closed shadow roots remain invisible to script inspection.
- **Frame-scoped clicks recipe**: If `click <id>` refuses an element in a nested frame (e.g. hit-testing reports `frame-clipped` or container obscuring), accumulate `getBoundingClientRect` offsets down the frame chain with `eval`, then `clickat X,Y` (or use `clickat --from-shot <path>` with image coordinates).
  ```bash
  # Accepts both comma and space formats:
  chrome-ctl clickat 65,238
  chrome-ctl clickat 65 238
  # Or click directly from screenshot image coordinates:
  chrome-ctl clickat --from-shot /tmp/view.webp 100 200
  ```
- **JS widgets & synthetic clicks**: On JS widgets (e.g. D2L/Brightspace LMS, rich dropdowns), synthetic `.click()` via `eval` often skips change/save event handlers. Prefer `click <id>` or `clickat` over `eval element.click()`.
- **Form verification before submit**:
  Always verify drafted choices before submitting any form or assessment:
  - `read --forms` renders options with `(•) CPU`, `( ) GPU`, `[x] Oval`, `[ ] Rect`.
  - `chrome-ctl formstate` lists all controls across all same-origin frames with `[frame]`, `checked`, `value`, `disabled`, `name`, `id`, `label`.
- Nested iframes: Coordinates are automatically translated across same-origin iframe boundaries for clicking.
- Long-polling SPAs: If a page has perpetual network activity (e.g. background chat or polling), `wait --network` automatically discounts requests pending longer than 10s so it does not hang indefinitely. You can customize this or rely on `wait --for-text`.

## Escape hatches

```bash
chrome-ctl describe 12
chrome-ctl describe 12 --frame f2
chrome-ctl describe "css=div[role=combobox]" --frame f1
chrome-ctl href 12                          # resolved absolute URL only
chrome-ctl href 12 --frame f2
chrome-ctl click 12 --timeout 10000         # timing breakdown: dispatch + DOM settle
chrome-ctl scroll down 0.75                 # 5s timeout; targets innermost scrollable frame
chrome-ctl scroll --to "div.quiz-summary"   # scroll directly to selector
chrome-ctl eval 'document.title'
chrome-ctl eval --raw 'location.href'
chrome-ctl eval --await 'fetch("/api/status").then(r => r.json())' --timeout 8000
```

- `describe` prints `<tag> "name" @x,y WxH` plus role, type, id, name, `href`/`src`, value, state, selector match count, and every attribute. Accepts `--frame <fN|name|selector>`.
- `eval` returns JSON-serialisable values; `--raw` unquotes a string, `--await` waits for a promise. Output is truncated at 100,000 characters and the expression is logged to the extension console. It runs over CDP, so it works even when page CSP blocks injected scripts — it is instant, skips humanisation, and can read anything the page can read. Never use it to bypass a confirmation or to scrape more than the task needs.
- Disable it entirely with `chrome-ctl daemon --stop && chrome-ctl daemon --start --no-eval`; the gate survives the daemon's background restart, and `feature disabled` means exactly that.
- `url`, `describe`, `clickat`, and `eval` are deliberate escapes, not the default path.

## Inline batching (`chrome-ctl batch`)

Run a fast, ad-hoc sequence of verified commands without recording or saving a macro.

```bash
chrome-ctl batch "click 5" "wait --for-text Saved" "click 7"
chrome-ctl batch "type 3 'Alice' --enter" "wait --network" "read"
```

- Each argument is a standard `chrome-ctl` subcommand and its flags.
- **Fail-fast with observation**: Verifies each step before executing the next. Stops immediately on the first error and automatically prints a fresh `snap` observation to stderr (unless `--snap-on-error=false`).
- **No auto-retry**: Never auto-retries a failed mutation (especially submits/payments).
- Use `batch` when you have a predictable 2–5 step sequence ("click option → wait for message → next") and want to avoid back-and-forth conversation latency.

## Bulk extraction

The zero-token workflow: page bodies go to files, not into the conversation.

```bash
chrome-ctl bulk urls.txt --out ./pages --wait 6000-9000
chrome-ctl bulk queries.txt --template 'https://www.bing.com/search?q={{item}}' --out ./serp
chrome-ctl bulk - --out ./pages --resume < urls.txt
chrome-ctl bulk urls.txt --out ./pages --limit 20 --min-bytes 500 --stop-on-empty
```

- One item per line; blanks and `#` comments are ignored; `-` reads stdin. `--template` URL-escapes each item into `{{item}}`.
- Items navigate in the automation tab; per item: navigate, network-quiet wait (falls back to DOM idle), jitter `--wait`, `read`. Files are `NNN-slug.md`; every attempt appends to `manifest.jsonl`.
- `--resume` skips URLs already marked `ok`. `--stop-on-empty` stops below `--min-bytes` (300) and `--max-repeats` (3) consecutive identical pages stop the run — both usually mean a rate limit or a login wall. `--network=false` switches to DOM-idle waiting; `--quiet` suppresses per-item stderr progress.
- stdout carries only the summary and `FAIL` lines. Check `manifest.jsonl` for detail.
- A shell loop works the same way without `bulk` — cap the list, stop on repeated content hashes, keep the jitter:

```bash
while read -r url; do
  chrome-ctl open "$url" >/dev/null
  chrome-ctl wait --network --timeout 15000 >/dev/null
  chrome-ctl read > "pages/$(date +%s%N).md"
  chrome-ctl wait 3000-7000
done < urls.txt
```

## Macros

Macros record successful CLI actions and replay from `~/.chrome-ctl/macros/<name>.json`.

```bash
chrome-ctl macro list
chrome-ctl macro start directory_search
chrome-ctl open "https://example.com/directory"
chrome-ctl snap
chrome-ctl type 1 "Software Agencies" --enter
chrome-ctl macro save --params search_term --description "Directory search"
chrome-ctl macro run directory_search --vars '{"search_term":"Design Studios"}'
```

- `macro start` → `OK: recording macro "…"`, `macro save` → `OK: saved macro "…" (4 steps, params: search_term)`, `macro run` → `OK: ran macro "…" (4 steps, 8.2s)`.
- `--params` is a comma-separated list of values to template; `--vars` is a JSON object of strings. Save only after verifying the flow worked.
- The daemon records the element descriptor before each action mutates the page, so navigation does not erase the target; replay resolves test ids, ARIA names, text, XPath, then CSS. Snapshot IDs are never stored.
- A long replay gets a 600 s budget; a step failure exits 1 with detail on stderr. See [Recover](#recover) and [Resume](#resume).

## Visuals and recordings

```bash
chrome-ctl shot                              # WebP into ~/.chrome-ctl/media
chrome-ctl shot --exact -o /tmp/view.webp    # full resolution, no downscaling
chrome-ctl shot --full -o /tmp/full.webp     # capture full scrollable page
chrome-ctl shot --som -o /tmp/view.webp      # IDs drawn on interactive elements
chrome-ctl shot --gray -o /tmp/verify.webp   # grayscale, for text checks
chrome-ctl rec start --fps 2                 # 1-2 fps
chrome-ctl rec stop --grid -o /tmp/boards.webp   # 3x4 contact sheet, no ffmpeg
chrome-ctl rec stop -o /tmp/clip.webm            # WebM, requires ffmpeg
```

- Each screenshot prints its capture timestamp and image hash: `OK: saved /tmp/view.webp (1024x628, 41 KB, hash a1b2c3d4 @ 15:04:05, viewport 1414x867, image 1024x628, scale 0.724)`. A sidecar `.meta.json` records dimensions for `clickat --from-shot`.
- If the image hash is identical to the previous capture right after a mutation, `shot` warns: `WARN: identical to previous capture; content may be composited in a subframe`.
- Storyboards are one WebP and the lowest-token way to review a sequence. WebM needs `ffmpeg` on `PATH` (or `CHROME_CTL_FFMPEG`); if it is missing, use `--grid`. Without `-o`, artifacts land in `~/.chrome-ctl/media`. If `captureVisibleTab` cannot grab the tab — minimized window, heavy canvas, another debugger owns the tab — `shot` retries through the debugger and appends `via CDP fallback (captureVisibleTab failed)`; if both fail, the error names both causes. Restore the window or ask the user. `rec` state does not survive a daemon restart; re-`rec start`.

## Google SSO

Google sign-in is click-only; `chrome-ctl` never types credentials.

```bash
chrome-ctl click <google_button_id>     # found from a snapshot
chrome-ctl sso --account user@example.com
chrome-ctl sso --timeout 30000          # 1000-60000 ms, default 15000
```

`--account` is optional; without it the first available account is picked. On `OK: signed in as … (redirect complete)` or `OK: OAuth popup closed (redirect pending)`, check `url`. On `SSO aborted: password required; complete sign-in manually`, or any CAPTCHA, passkey, or 2FA challenge, follow [Stop and ask](#stop-and-ask).

## Site etiquette and web pitfalls

- Warm up before outreach: open the page, `scroll down 0.6`, `wait 2000-4000` before clicking Connect, Message, or Follow. Do not jump from URL to final submit in under two seconds; keep randomized `wait 3000-7000` between repeated iterations.
- One task per tab where practical; never run parallel actions in the same tab.
- Never guess deep-link parameters. Navigate once through the UI and read `url`; apps key on their own names (`?siteUrl=` vs `?websiteId=`) and a guessed parameter renders a page of silent "Invalid request" states.
- After every navigation, check `url` and the title for redirects, login walls, and soft 404s before reading. Handle consent and cookie banners first — they intercept the clicks meant for the page.
- Empty results, HTTP 429 (`net --errors`), or a CAPTCHA are rate-limit signals: back off, do not hammer. Infinite scroll never ends by itself — cap pages and stop on repeated content hashes.
- Pagination belongs to the site's own next control or a parameter you observed in the UI, never a guessed page number.
- Canvas and chart data is not in the DOM: use `net` or `eval`. A screenshot is not proof of the numbers.
- Controlled React/Vue inputs may ignore text until focused — trust `(verified)` versus `reads empty`, then click the field and retry.

## Output contract

| | |
| :--- | :--- |
| Exit codes | `0` success-or-dispatched, `1` failure. Nothing else exists. |
| stdout | Results and proof lines (`OK: …`, `WARN: …`, snapshots). |
| stderr | `Error: <text>` for most failures; per-item `bulk` progress; the snapshot attached to `MACRO STEP FAILED`. |
| Silent exit 1 | `status` when the daemon is unreachable, and a failed `macro run` — no `Error:` line, so check the code **and** line 2. |
| State dir | `${CHROME_CTL_HOME:-~/.chrome-ctl}` → `auth_token`, `daemon.pid`, `daemon.log`, `media/`, `macros/` (mode `0700`). |
| Env | `CHROME_CTL_HOME`, `CHROME_CTL_ADDR` (default `127.0.0.1:9229`), `CHROME_CTL_FFMPEG`. |
| Defaults | `wait --timeout 15000`, `wait --quiet-ms 500`, `snap --max-elements 400`, `read --max-links 200`, `read --max-tables 10`, `shot --max-width 1024 --quality 65`, `eval --timeout 5000`, `sso --timeout 15000`, `rec start --fps 2`, `net --limit 50`, `bulk --wait 6000-9000 --min-bytes 300 --max-repeats 3`. |
