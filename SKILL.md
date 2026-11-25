---
name: chrome-ctl
description: Drive the user's authenticated Chrome from the CLI: plan, operate, verify each action against real output, recover from dispatch failures, stop for credentials and destructive steps, and resume interrupted work. Snapshots, SPA-aware waits, bulk extraction, and local macros run from an unfocused background window.
---

# Chrome Control (`chrome-ctl`)

`chrome-ctl` drives the user's real, signed-in Google Chrome via a local daemon and Manifest V3 extension using trusted input, so sessions, cookies, and fingerprints come from their normal profile. `make build` produces `./bin/chrome-ctl` (releases ship `dist/chrome-ctl-<os>-<arch>`); prefix examples with your binary path if it is not on `PATH`.

## Four rules that never bend

1. **Snapshot first, never HTML.** `snap` returns a compact map of interactive elements (`[1]`, `[2]`). Never request raw HTML or paste a DOM into the conversation.
2. **IDs die with the document.** Navigation, reload, or a new tab resets IDs and never reuses them. Act only on IDs from the latest snapshot.
3. **Exit 0 is not proof.** It means input was dispatched, not accepted. Verify against the output text — see [Verify](#verify).
4. **Secrets and humans are hand-back points.** Passwords, one-time codes, CAPTCHA, 2FA, passkeys, wallets, downloads, and anything destructive or spendable stop the run — see [Stop and ask](#stop-and-ask).

## Task lifecycle

Name the phase you are in when narrating progress; do not start the next until the current one has evidence.

| Phase | Done when |
| :--- | :--- |
| 1. Plan | Goal, terminal proof, and vehicle written down; daemon `RUNNING`, extension `CONNECTED`, automation window yours, `macro list` checked. |
| 2. Operate | Each mutation is followed by its observation; no stale IDs used. |
| 3. Verify | Predicted output line matches actual output. |
| 4. Recover | Errors mapped to their next command, not retried blind. |
| 5. Stop and ask | Blocker captured with evidence and handed to the user, or nothing was blocked. |
| 6. Resume | Journal updated; a fresh session could continue from it. |

## Plan and preflight

Write one line before any browser command: the goal, and **what output proves it is done** (a file path, URL, or visible string). That line is your check in [Verify](#verify) and record in [Resume](references/bulk-and-macros.md#resume).

```bash
chrome-ctl status                       # daemon + extension, three lines (read line 2!)
chrome-ctl daemon --start               # only if status exits 1
chrome-ctl window ensure                # reuse or create your unfocused window
chrome-ctl window list                  # * focused, (automation) yours
chrome-ctl macro list                   # saved macros cost zero tokens
```

- `status` exits `1` silently when the daemon is unreachable, and exits **`0` even while printing `Chrome Extension: DISCONNECTED`** — always read line 2. If disconnected, ask the user to load or reload `extension/dist` at `chrome://extensions`; pairing is automatic.
- Confirm `window ensure` reports `unfocused` and `window list` marks `(automation)` on yours (`*` elsewhere). `~/.chrome-ctl` holds `auth_token`, `daemon.pid`, `daemon.log`, `macros/`, and `media/`.

| Vehicle | Use when | Why |
| :--- | :--- | :--- |
| Ad-hoc commands | A few pages, judgement needed | Output enters the conversation where you can read it. |
| `batch "<cmd1>" "<cmd2>"` | Predictable 2–10 step sequence | Fast sequential execution; fails fast with fresh `snap`; no record/save ceremony. |
| `bulk --out <dir>` | Known list, no judgement per page | Bodies go to files (`NNN-slug.md` + `manifest.jsonl`); token cost stays flat; resumable. |
| `macro` | Verified sequence run more than once | Replays locally from resilient selectors; zero model tokens. |

There is **no `--dry-run`** anywhere and **no `macro delete`** (`macro` has `start|save|list|run`; retire by re-saving the same name). Rehearse risky flows against local fixtures: `python3 -m http.server 8080 --directory tests/fixtures` then `chrome-ctl open "http://localhost:8080/test_form.html"`.

## Operate

One mutation, one observation. Prefer single-turn flags (`--wait`, `--snap`, `--diff`) and bounded reads over multi-turn polling.

```bash
chrome-ctl open "https://example.com" --wait --snap          # navigate + wait + initial snap
chrome-ctl open "https://app.example.com" --wait --for-text "Dashboard" --snap
chrome-ctl url                                               # cheap 1-line "where am I"
chrome-ctl snap                                              # SNAP <n> | TAB [<tabId>] <title> | <url>
chrome-ctl snap --diff                                       # compact delta vs previous snap
chrome-ctl click "Submit"                                    # 1-shot target click (text or css=#sel); no prior snap needed
chrome-ctl click 5 --snap                                    # click + compact DIFF (+N added, ~M changed)
chrome-ctl type 3 "Hello" --enter --snap                     # human cadence + read-back verification + DIFF
chrome-ctl press Enter --snap                                # key press + compact DIFF
chrome-ctl scroll down --snap                                # scroll + compact DIFF (or scroll --to "<sel>")
chrome-ctl wait --network                                    # wait until requests settle (or --for-text "...")
chrome-ctl read --max-chars 4000                             # bounded markdown (--full removes cap)
chrome-ctl formstate --filled                                # populated/checked controls (--full removes cap)
```

- **1-shot clicks & consequence receipts:** `click "Text"` and `click "css=#sel"` resolve in-page without a prior `snap`. Inline receipts (`-> selected (•)`, `-> opened dialog "Delete"`, `-> navigated to ...`) confirm simple consequences without another `snap`.
- **Turn-saving `--snap` & `--diff`:** `click`, `type`, `press`, and `scroll` with `--snap` (and `snap --diff`) emit compact `DIFF` deltas (pure coordinate shifts collapse into `, N moved`). Pair `open --wait` / `--for-text` with `--snap` so you don't land on `(still loading)`.
- **Safe output caps:** `read` supports `--max-chars <n>` / `--full` (plus `--forms`, `--tables`, `--links`, `--json`, element ID/selector scoping); `formstate` supports `--filled`, `--frame`, `--max-controls <n>`, and `--full`.
- **Window, IDs & motion:** Commands target the automation window's active tab — never touch user tabs/windows. A new `SNAP <n>` is the only proof of a new observation. `click`/`type`/`press`/`scroll` return a `seed` for deterministic replay. Check `macro list` before repeatable flows; use `shot --som` only when icons/canvas make text ambiguous.

## Verify

Only exit codes `0` and `1` exist. Failures print `Error: <text>` to stderr — except `status` (daemon unreachable) and failed `macro run` / `batch`, which exit `1` silently with details on stderr.

| Command | Proof it worked | What it still does not prove |
| :--- | :--- | :--- |
| `type` | `OK: Typed N chars into [id] (verified; …).` | Site accepted value on submit (no `verified` = treat as unverified). |
| `type` | `WARN: Typed N chars … reads empty` / `reads "…"` | Rejected (readonly/controlled) or wrong target; exit code is still 0. |
| `click` | `OK: clicked [id] <tag> "…" @x,y -> <consequence>` | Hit coords and immediate consequence; skip `snap` if consequence confirms mutation. |
| `wait` | `OK: network quiet after …` / `text "…" appeared` / `idle after …` | Any `WARN: … State unknown.` is a timeout, never proof of static state. |
| `open` | `OK: navigated [id] <title> - <url>` | Check for redirect or `(still loading)` when `--wait` was omitted. |
| `snap` | Higher `SNAP <n>` counter than last time | Only re-observes; proves nothing about the prior action. |
| `batch` | `[1/N] OK: …` for each step | Stops on first error with fresh `snap` on stderr; never auto-retries. |
| `bulk` | `OK: N scraped, M failed -> <dir> (manifest.jsonl)` | Exits `1` if any item failed; bodies are in files, not stdout. |
| `macro run` | `OK: ran macro "<name>" (N steps, Xs)` | Verify site end state with `snap` after. |
| `sso` | `OK: signed in as <email> (redirect complete)` | Read `url` to confirm you are back on the site. |
| `shot` | `OK: saved <path> (WxH, size, …)` | Pixels prove appearance, not data (`net` or `eval` for values). |

`press`, `scroll`, and `switch` emit dispatch receipts (`OK: pressed …`, `OK: scrolled …`, `OK: switched …`). After a submit, prove consequence (`url`, `wait --for-text`, `snap`/`read`); verify drafts before high-stakes Post, Send, or Submit.

## Recover

Match literal text, then run the named command — never improvise a blind retry.

| What you see | Next move |
| :--- | :--- |
| `cannot read auth token from ~/.chrome-ctl/auth_token…` | `daemon --start`, then `status`. |
| `Daemon: STOPPED` or `Chrome extension not connected` | Start daemon; for extension, ask user to load/reload `extension/dist`, require `CONNECTED`. |
| `DISPATCHED, …. State unknown` / `DISPATCHED, the extension stopped waiting for an ack` | `snap` or `url` first. Effect visible → continue. Absent on Submit/Send/delete/purchase → [Stop and ask](#stop-and-ask). |
| `snapshot stale: page changed since snapshot <n>; run snap` | `snap`, re-locate target by name/text, use new ID. |
| `element [<id>] not found` / `run snap first` / `element not found (<target>)` | Escalate: `snap` → `snap --all` → `snap --deep` → `snap --frames` → `describe "css=…"` → `press Tab` → `clickat X,Y`. |
| `element [<id>] is not interactable (<reason>)` | `scroll`, dismiss covering banner, or click `aria-expanded` parent, then `snap`. |
| `selector "<css>" matched no elements` / `invalid selector` | `snap --deep`, or scope from an ID with `describe <id>`. |
| `tab <id> not found` | `tabs` (or `tabs --window <id>`), then `switch <id>` or `window ensure`. |
| `window <id> not found` / `has no active tab` / `no focused window found` | `window list`, then `window ensure` or `window use <id>`. |
| `debugger <op> failed: <reason>` | Retry once (navigation/DevTools detaches it). If repeated, ask user to close DevTools or competing debugger tool. |
| `failed to navigate the active tab` / `invalid url` / `url is required` | Check `url`, then `open` again. |
| `<protocol> pages do not allow content scripts` | `chrome:`, `chrome-extension:`, `chrome-untrusted:`, `edge:`, `about:`, `devtools:`, `view-source:` are unreachable. Ask user. |
| `unknown key: <key>` | Use `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`, `Space`, `Home`, `End`, `PageUp/Down`, `ArrowUp/Down/Left/Right`, or 1 char. |
| `SSO timeout: no Google OAuth popup detected` / `did not complete` | `snap`: did popup close, show consent, or need a human? |
| `SSO aborted: password required; complete sign-in manually` | Hand back via [Stop and ask](#stop-and-ask). Never type a password. |
| `BATCH STEP FAILED: step <n> ("<cmd>"): <reason>` + snap | Read stderr snapshot to see why it failed; fix or continue manually, never retry blind. |
| `MACRO STEP FAILED: macro "<name>" step <n>: <reason>` + snap | Read stderr snapshot, repair step manually, re-record and `macro save` same name. Never rerun unchanged. |
| `feature disabled` from `eval` | Daemon runs with `--no-eval`; ask user before `daemon --stop && daemon --start`. |

Not errors: `OMITTED: <n> candidates`, `net` empty-log note (`collected only while the debugger is attached`), and `WARN: DOM idle wait timed out`. Check `~/.chrome-ctl/daemon.log` on daemon restarts; point at the right window with `window use <id>`.

## Stop and ask

Hand back when you observe: (1) a `type=password` field or password/passphrase/API-key/card/OTP input; (2) `SSO aborted: password required…`, CAPTCHA, passkey, 2FA, or wallet; (3) Delete, Purchase, Pay, Post, Send, Grant access, submitting a graded test/quiz, or anything spendable/irreversible; (4) a download control (this build cannot verify downloads landed); (5) data about to leave to a third party or out-of-scope exfiltration.

Sequence — five steps, no improvisation:

1. **Freeze.** Issue no further mutating command. Reads only.
2. **Capture.** `snap` and `url`; add `shot --gray -o /tmp/blocked.webp` if the blocker is visual.
3. **Preserve.** Leave the tab where the user can finish it. Do **not** `window clear`, `window close`, or navigate away.
4. **Ask once**, with: the one-line goal, current `url` and tab/window IDs from `SNAP`, literal evidence line, the single action needed, and "I will not proceed until you confirm."
5. **Re-`snap`** after they confirm — manual work invalidates every held ID.

The CLI has **no approval gate, no `--confirm`, no `--dry-run`, and no pause** (`sso` refuses credentials; `--no-eval` disables script exec). Click a download control at most once, then ask. Never invent, guess, or paste a secret, or put one in a URL/query string.

## Reference docs

Read on demand when a task uses these capabilities:

| Reference | Covers |
| :--- | :--- |
| [references/forms-and-frames.md](references/forms-and-frames.md) | `formstate` (`--frame`, `--filled`, `--max-controls`, `--full`), nested iframes, `clickat --from-shot`, JS/React/Vue controlled inputs, `describe`, `href`, `eval` escape hatch. |
| [references/bulk-and-macros.md](references/bulk-and-macros.md) | `batch`, `bulk` flags/resume/circuit breakers, `macro` recording/params/replay/sagas, [Resume](references/bulk-and-macros.md#resume) recipes, `tasks/*.journal.jsonl` convention. |
| [references/visuals-and-sso.md](references/visuals-and-sso.md) | `shot` (`--som`, `--gray`, `--max-width`, `--exact`, `--full`), `rec` (`--grid`, WebM), `sso`, `net`, windows/waits, Site Etiquette & Web Pitfalls, [Output contract & defaults](references/visuals-and-sso.md#output-contract-and-defaults). |
