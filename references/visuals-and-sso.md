# Visuals, SSO, Network, Site Etiquette, and Output Contract

Detailed reference for screenshots (`shot`), recordings (`rec`), Google OAuth (`sso`), network inspection (`net`), window/wait nuances, web safety etiquette, and the complete output contract. See [../SKILL.md](../SKILL.md) for core lifecycle rules.

## Screenshots

Use `shot --som` only when icons, canvas, or layout make text snapshots ambiguous — a screenshot costs far more tokens than `snap`.

```bash
chrome-ctl shot                              # WebP into ~/.chrome-ctl/media
chrome-ctl shot --exact -o /tmp/view.webp    # 1:1 scale, no downscaling
chrome-ctl shot --full -o /tmp/full.webp     # full scrollable page beyond viewport
chrome-ctl shot --som -o /tmp/view.webp      # Set-of-Mark ID badges matching snap IDs
chrome-ctl shot --gray -o /tmp/verify.webp   # grayscale, best for text/receipt checks
chrome-ctl shot --max-width 1280 --quality 80 -o /tmp/hi.webp
```

- **Flags & defaults:** `--max-width <256-4096>` (default `1024`), `--quality <1-100>` (default `65`), `--som`, `--gray`, `--exact`, `--full`, `-o/--output <path>` (without `-o`, saved to `~/.chrome-ctl/media` with mode `0600`).
- **Receipt & sidecar metadata:** Prints capture timestamp, SHA/hash, and scale:
  `OK: saved /tmp/view.webp (1024x628, 41.0 KB, hash a1b2c3d4, capture 15:04:05, viewport 1414x867, image 1024x628, scale 0.724[, grayscale][, 42 badges])`
  A sibling `<path>.meta.json` records `viewportWidth`, `viewportHeight`, `imageWidth`, `imageHeight`, and `scale` for [Clickat and screenshot coordinates](forms-and-frames.md#clickat-and-screenshot-coordinates) (`clickat --from-shot`).
- **Identical capture warning:** If the image hash matches the previous capture right after a mutation, `shot` prints:
  `WARN: identical to previous capture; content may be composited in a subframe`
- **CDP fallback & minimized windows:** Chrome can only capture a visible tab. If `captureVisibleTab` fails (minimized window, heavy canvas, competing debugger), `shot` retries via `Page.captureScreenshot` and appends `via CDP fallback (captureVisibleTab failed)`. If both fail, the error names both causes — un-minimize the window (`window list`) or ask the user.
- **Evidence limits:** Pixels prove visual layout, not underlying data values on charts/canvas — use `net` or `eval` for numbers.

## Recordings (`rec`)

```bash
chrome-ctl rec start --fps 2                       # 1-2 fps (default 2)
chrome-ctl rec stop --grid -o /tmp/boards.webp     # 3x4 WebP contact sheet, no ffmpeg needed
chrome-ctl rec stop -o /tmp/clip.webm              # VP9 WebM, requires ffmpeg
```

- **Storyboards (`--grid`):** Compiles frames into one `3x4` WebP contact sheet (`~340` vision tokens) — the lowest-token way to review an async multi-step flow. Requires no external tools.
- **WebM (`rec stop` without `--grid`):** Encodes VP9 WebM; requires `ffmpeg` on `PATH` or `CHROME_CTL_FFMPEG`. If `ffmpeg` is missing, use `--grid`.
- **State lifetime:** `rec` state does **not** survive a daemon restart — re-run `rec start`. Keep the automation window un-minimized (`window list` must not show `(minimized)`). Without `-o`, artifacts land in `~/.chrome-ctl/media`.

## Google SSO (`sso`)

Google sign-in is click-only; `chrome-ctl` never types credentials.

```bash
chrome-ctl click <google_button_id>     # found from snap
chrome-ctl sso --account user@example.com
chrome-ctl sso --timeout 30000          # 1000-60000 ms, default 15000
```

- `--account` is optional; without it, the extension clicks the first available account in the `accounts.google.com` popup.
- **Receipts:** `OK: signed in as user@example.com (redirect complete)`, `OK: OAuth completed (redirect complete)`, or `OK: OAuth popup closed (redirect pending)`. Always verify `url` afterwards to confirm landing on the destination page.
- **Hand-back triggers:** On `SSO aborted: password required; complete sign-in manually`, or any CAPTCHA, passkey, or 2FA prompt, immediately follow [Stop and ask](../SKILL.md#stop-and-ask). On `SSO timeout: ...`, run `snap` to check whether the popup closed, showed a consent screen, or needs human input.

## Windows, tabs, waits, and network log (`window`, `tabs`, `switch`, `wait`, `net`)

### Windows, focus, and tabs

```bash
chrome-ctl window ensure                    # reuse or create unfocused automation window
chrome-ctl window list                      # * focused, (automation) yours, (maximized|minimized|fullscreen)
chrome-ctl window new "https://example.com" # always a fresh window (--foreground focuses it)
chrome-ctl window use <id|focused>          # target an existing window
chrome-ctl window clear                     # stop targeting a dedicated window
chrome-ctl window close 12                  # close window (--force overrides focused guard)
chrome-ctl tabs [--window <id>]             # list open tabs
chrome-ctl switch <tab_id>                  # switch tab; automation target follows that tab's window
```

- Never use `window new --foreground`, `window use focused`, or `window close --force` unless the user explicitly asked you to drive their own window — state which window you took, and run `window clear` when done.
- `daemon --start` reports `Daemon: STARTED (pid …, 127.0.0.1:9229)` or `ALREADY RUNNING`, `Token file: ~/.chrome-ctl/auth_token`, and `Chrome extension: pairs automatically`.
- `window ensure` is idempotent and prints `OK: using automation window [id] (tab [id], 1 tab, unfocused)` (or `created`) without stealing focus — safe even while the user watches media.
- `window close` refuses a focused window (`window <id> is focused; pass --force …`).
- Keep the automation window visible (not `(minimized)`), or `shot`/`rec` cannot capture.

### Waits (`wait`)

```bash
chrome-ctl wait 2500                        # fixed sleep (ms, max 600000)
chrome-ctl wait 3000-7000                   # uniform random jitter range (ms)
chrome-ctl wait "#results"                  # wait for CSS selector (10s timeout)
chrome-ctl wait idle                        # DOM quiet + document complete, NOT network (10s timeout)
chrome-ctl wait --network                   # no requests in flight & none started for --quiet-ms (500ms)
chrome-ctl wait --for-text "Saved" --ignore-case
chrome-ctl wait --network --for-text "Done" --timeout 20000 --quiet-ms 500
```

- **SPA & long-polling guidance:** SPAs render a shell, go DOM-quiet, then fetch for seconds more — `wait idle` alone routinely returns too early; prefer `--network` or `--for-text` (combinable under one `--timeout`, default `15000` ms, max `30000` ms).
- **Long-polling cutoff:** `wait --network` automatically excludes pending requests older than 2s (`DEFAULT_LONG_POLL_MS = 2000`) so background chat/streams do not hang forever; for perpetual streams, prefer `--for-text` with an explicit `--timeout`.
- **0ms / DevTools caveats:** `--network` needs `chrome.debugger`; if DevTools owns the tab, it reports that network events could not be enabled instead of pretending quiet. If `wait --network` prints `WARN: network quiet after 0ms (0 requests observed)`, no traffic was observed — not proof of quiet; verify state. Any `WARN: … State unknown.` is a timeout, never proof the page is static.

### Network inspection (`net`)

```bash
chrome-ctl net                              # requests seen while debugger attached (limit 50)
chrome-ctl net --errors --since 5m --limit 50
chrome-ctl net --json                       # raw JSON (PageNetResult) instead of TSV
```

- `--errors` filters to network errors or HTTP status `>= 400`. `--since` accepts positive Go durations (`30s`, `5m`).
- **Attachment scope:** `net` observes only what happened while `chrome.debugger` was attached (roughly during actions and `wait --network`). An empty output with `note: no network data yet…` means nothing was observed, not that the page made zero requests; if DevTools owns the tab, logs cannot be collected.
- Network entries mirror to extension session storage, surviving service-worker suspension but **not** a Chrome quit.

## Site etiquette and web pitfalls

- **Warm up before outreach:** Open the page, `scroll down 0.6`, `wait 2000-4000` before clicking Connect, Message, or Follow. Do not jump from URL to final submit in under 2 seconds; keep randomized `wait 3000-7000` between repeated iterations.
- **Tab isolation:** One task per tab where practical; never run parallel actions in the same tab.
- **Never guess deep-link parameters:** Navigate once through the UI and read `url`; apps key on their own names (`?siteUrl=` vs `?websiteId=`) and guessed parameters trigger silent "Invalid request" states.
- **Check after every navigation:** Inspect `url` and page title for redirects, login walls, and soft 404s before reading. Handle consent and cookie banners first — they intercept clicks meant for the page.
- **Respect rate limits & infinite scroll:** Empty results, HTTP 429 (`net --errors`), or a CAPTCHA are rate-limit signals — back off, never hammer. Infinite scroll never terminates on its own — cap pages and stop on repeated content hashes.
- **Pagination:** Use the site's own next control or an observed UI parameter, never a guessed page number.
- **Canvas & charts:** Chart/canvas values are not in the DOM — inspect via `net` or `eval`; a screenshot is not proof of numbers.
- **Controlled React/Vue inputs:** May ignore text until focused — trust `(verified)` vs `reads empty` (see [Verify](../SKILL.md#verify)), click the field, and retry.

## Output contract and defaults

| Aspect | Contract / Default |
| :--- | :--- |
| Exit codes | `0` success-or-dispatched, `1` failure. Nothing else exists. |
| `stdout` | Results and proof lines (`OK: …`, `WARN: …`, snapshots, `bulk` summary & `FAIL` lines). |
| `stderr` | `Error: <text>` for most failures; per-item `bulk` progress; `net` notes; fresh snapshot attached to `BATCH STEP FAILED` / `MACRO STEP FAILED`. |
| Silent exit `1` | `status` when daemon unreachable, failed `macro run`, and failed `batch` (no `Error:` prefix; read line 2 or stderr detail). |
| State directory | `${CHROME_CTL_HOME:-~/.chrome-ctl}` (mode `0700`) → `auth_token` (`0600`), `daemon.pid`, `daemon.log`, `media/` (`0600`), `macros/` (`0600`). |
| Environment | `CHROME_CTL_HOME`, `CHROME_CTL_ADDR` (default `127.0.0.1:9229`), `CHROME_CTL_FFMPEG`. |
| Defaults & caps | `wait --timeout 15000` (max `30000`), `wait --quiet-ms 500`, `snap --max-elements 400`, `read --max-links 200 --max-tables 10` (plus `--max-chars <n>` / `--full`), `formstate --max-controls <n>` / `--full` / `--filled`, `shot --max-width 1024 --quality 65` (`256–4096`, `1–100`), `eval --timeout 5000` (max `30000`, `100000` char output cap), `type --typo-rate 0.02` (max `2000` chars), `scroll` `0.8` (5s timeout), `sso --timeout 15000` (`1000–60000`), `rec start --fps 2` (`1–2`), `net --limit 50`, `bulk --wait 6000-9000 --min-bytes 300 --max-repeats 3`. |
