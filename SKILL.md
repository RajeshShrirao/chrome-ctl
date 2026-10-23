---
name: chrome-ctl
description: Control the user's authenticated Chrome browser with low token overhead, human-like actions, Google SSO handling, optimized screenshots/recordings, and reusable local macros.
---

# Chrome Control (`chrome-ctl`)

`chrome-ctl` drives the user's real, already-signed-in Google Chrome browser through a local daemon and a Manifest V3 extension. Actions use trusted input, so sessions, cookies, and hardware fingerprints are inherited from the user's normal profile.

Use the installed `chrome-ctl` binary (or `./bin/chrome-ctl` from a source checkout). The daemon and extension must both be connected before any browser command will work.

## Core principles

1. Never ask for or scrape raw HTML. Use `chrome-ctl snap` to get a compact map of interactive elements with numeric IDs (`[1]`, `[2]`, ...).
2. Use `chrome-ctl snap --forms` when filling registrations: it limits output to inputs, selects, textareas, and submit controls.
3. Chain `--snap` after UI-changing actions (`click`, `type`) to get the next element map in one turn instead of issuing a separate snapshot.
4. Prefer text snapshots. Reach for `chrome-ctl shot --som` only when icons, canvas UI, or visual layout make text ambiguous, or when you need visual proof. Set-of-Mark badges reuse the same IDs as `snap`.
5. Run `chrome-ctl macro list` before a multi-step or repeatable task. Reusing a saved macro consumes no model tokens.

## Standard loop

```bash
chrome-ctl status                       # daemon + extension health
chrome-ctl tabs                         # list open tabs
chrome-ctl open "https://example.com"   # navigate the active tab
chrome-ctl open "https://example.com" --new
chrome-ctl switch 2                     # make tab [2] active
chrome-ctl snap                         # interactive elements in view
chrome-ctl snap --all                   # include off-screen elements
chrome-ctl snap --forms                 # form controls only
chrome-ctl read                         # readable page markdown
chrome-ctl read 7                       # markdown for element [7]
```

Act on IDs from the latest snapshot:

```bash
chrome-ctl click 5                       # humanized click
chrome-ctl click 5 --snap                # click, then fresh snapshot
chrome-ctl click 5 --double              # double-click
chrome-ctl type 3 "Hello" --enter        # human cadence, then Enter
chrome-ctl type 3 "Hello" --snap         # type, then fresh snapshot
chrome-ctl upload 9 ./logo.png           # attach a local file
chrome-ctl press Enter
chrome-ctl press a --modifier meta       # repeat --modifier as needed (ctrl, alt, shift, meta)
chrome-ctl scroll down 0.8               # optional amount in viewport heights
chrome-ctl scroll up
chrome-ctl wait 2500                     # fixed milliseconds
chrome-ctl wait 3000-7000                # random duration in range
chrome-ctl wait "#results"               # wait for a CSS selector
chrome-ctl wait idle                     # DOM quiet + document complete
```

`wait idle` observes DOM mutations and `document.readyState`, not network traffic. After every action that can change the page, either chain `--snap` or run `snap` again before choosing the next ID.

## Google SSO

Google sign-in is click-only. `chrome-ctl` never types credentials.

```bash
# 1. Find and click the site's Google button from a snapshot
chrome-ctl click <google_button_id>

# 2. When the accounts.google.com popup appears, complete the picker
chrome-ctl sso --account user@example.com
chrome-ctl sso --timeout 30000          # optional, 1000-60000 ms
```

`--account` is optional; without it the first available account is chosen. If `sso` reports that a password is required, or a CAPTCHA, passkey, or 2FA challenge appears, stop immediately, take a snapshot or screenshot, and ask the user to finish the sign-in manually. Never invent, guess, or type a password or verification code.

## Macros

Macros record successful CLI actions and replay locally from `~/.chrome-ctl/macros/<name>.json`.

```bash
chrome-ctl macro list

chrome-ctl macro start directory_search
chrome-ctl open "https://example.com/directory"
chrome-ctl snap
chrome-ctl type 1 "Software Agencies" --enter
chrome-ctl macro save --params search_term --description "Directory search"

chrome-ctl macro run directory_search --vars '{"search_term":"Design Studios"}'
```

`--params` is a comma-separated list of values to template; `--vars` is a JSON object of string values. Save only after verifying the flow worked. On `MACRO STEP FAILED` (exit code 1), the CLI prints the failing step and a fresh snapshot to stderr. Read that snapshot, repair the step with normal commands, then re-record the corrected flow under the same name and save again to replace the macro.

## Visuals and recordings

```bash
chrome-ctl shot                              # WebP into ~/.chrome-ctl/media
chrome-ctl shot --som -o /tmp/view.webp      # IDs drawn on interactive elements
chrome-ctl shot --gray -o /tmp/verify.webp   # grayscale, useful for text checks

chrome-ctl rec start --fps 2
# ... perform the workflow ...
chrome-ctl rec stop --grid -o /tmp/storyboard.webp   # 3x4 contact sheet, no ffmpeg
chrome-ctl rec stop -o /tmp/clip.webm                # WebM, requires ffmpeg
```

Storyboards are a single WebP and the lowest-token way to review a sequence. WebM export needs `ffmpeg` on `PATH` (or `CHROME_CTL_FFMPEG`); if it is missing, use `--grid` instead. Without `-o`, artifacts land in `~/.chrome-ctl/media`.

## Pacing and anti-detection

- Warm up before outreach: open the profile or page, `scroll down 0.6`, then `wait 2000-4000` before clicking Connect, Message, or Follow.
- Do not jump from URL to final submit in under two seconds.
- Insert randomized pauses (`wait 3000-7000`) between repeated outreach or registration iterations.
- Before a high-stakes final Post, Send, or Submit, verify the draft with `snap` or `shot --gray`, then take a confirmation snapshot after submitting.
- Keep one task per tab where practical, and avoid parallel actions in the same tab.

## Error recovery

```bash
chrome-ctl status              # STOPPED/DISCONNECTED tells you what is missing
chrome-ctl daemon --start      # start the local daemon in the background
chrome-ctl snap                # re-observe after any failed action
```

- If the daemon is stopped, run `chrome-ctl daemon --start` and check `status` again.
- If the extension is disconnected, ask the user to load/reload `extension/dist` from `chrome://extensions` and confirm the token in the popup.
- On `element not found` or a stale ID, `snap` (or `snap --all`) and retry with the current ID rather than guessing.
- If `sso` times out, `snap` to see whether the popup closed, a consent screen appeared, or manual action is needed.
- On `MACRO STEP FAILED`, follow the macro repair flow above; do not rerun the same macro unchanged.
