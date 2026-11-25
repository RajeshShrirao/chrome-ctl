# Batching, Bulk Extraction, Macros, and Resume

Detailed reference for inline `batch`, high-volume `bulk` scraping, local `macro` recording/replay/sagas, and session recovery. See [../SKILL.md](../SKILL.md) for core rules and recovery error mappings.

## Inline batching (`batch`)

Run a predictable 2–10 step sequence without recording or saving a macro:

```bash
chrome-ctl batch "click 5" "wait --for-text Saved" "click 7"
chrome-ctl batch "type 3 'Alice Smith' --enter" "wait --network" "read --max-chars 4000"
chrome-ctl batch --snap-on-error=false "click 5" "press Enter"
```

- **Subcommand syntax:** Each argument is a standard `chrome-ctl` subcommand and flags (supports single/double quotes and backslash escapes). Nested `batch` inside `batch` is rejected.
- **Fail-fast with observation:** Verifies each step before running the next, emitting `[<i/N>] <stdout>` per completed step. On the first failure, it stops immediately, prints `BATCH STEP FAILED: step <n> ("<cmd>"): <reason>` plus a fresh `snap` to stderr (unless `--snap-on-error=false`), and exits `1`.
- **No auto-retry:** `batch` never auto-retries a failed mutation (especially submits or payments). Inspect the stderr snapshot before deciding the next step.

## Bulk extraction (`bulk`)

`bulk` keeps page bodies out of the conversation by writing markdown files directly to disk and streaming only failures and a one-line summary to stdout.

```bash
chrome-ctl bulk urls.txt --out ./pages --wait 6000-9000
chrome-ctl bulk queries.txt --template 'https://www.bing.com/search?q={{item}}' --out ./serp
chrome-ctl bulk - --out ./pages --resume < urls.txt
chrome-ctl bulk urls.txt --out ./pages --limit 20 --min-bytes 500 --stop-on-empty
chrome-ctl bulk urls.txt --out ./pages --concurrency 4 --block-media
```

- **Input list (`<file|->`):** One item per line; blank lines and `#` comments are ignored; `-` reads stdin. `--template` URL-escapes each item into `{{item}}`.
- **Flags and defaults:**
  - `--out <dir>` (required): destination for `NNN-slug.md` files (atomic temp-file + fsync + rename; 1-based index keeps names stable across resumes) and `manifest.jsonl`.
  - `--wait <ms|ms-ms>` (default `6000-9000`): jitter delay between items.
  - `--limit <n>` (default `0` = unlimited): stop after `n` items.
  - `--min-bytes <n>` (default `300`) and `--stop-on-empty` (default `true`): stop if a scraped markdown body is shorter than `--min-bytes`.
  - `--max-repeats <n>` (default `3`): stop after `n` consecutive exact (SHA-256) or near-duplicate (64-bit word-3-shingle SimHash LSH, Hamming distance ≤ 3) pages — catches rate-limit templates and login walls that vary slightly.
  - `--resume` (default `false`): skip URLs already marked `ok: true` in `<out>/manifest.jsonl`.
  - `--network` (default `true`): wait for network quiet (15s timeout, falls back to 10s DOM idle); `--network=false` skips directly to DOM-idle waiting.
  - `--block-media` (default `false`): blocks images, fonts, and video/audio via CDP `Network.setBlockedURLs` for the duration of the run, clearing automatically on exit.
  - `--concurrency <1-8>` (default `1`): opens up to `N` parallel background worker tabs inside the automation window (sharing per-host pacing/circuit breakers and mutex-guarded manifest writes) and closes worker tabs on exit.
  - `--quiet` (default `false`): suppress per-item progress on stderr.
- **Per-host pacing, retries, and circuit breakers:**
  - **AIMD pacing:** Per-host rate controller starts/caps at `4.0` req/s (min `0.1` req/s), adds `+0.25` on success, and halves (`×0.5`) on failure.
  - **Circuit breaker:** Opens per host after `4` consecutive failures for a `30s` cooldown, then admits one half-open probe. Open-breaker failure logs `circuit open for <host> after 4 consecutive failures`.
  - **Transient retries:** Retries transient RPC/transport failures (`ErrConnection`, `ErrDispatchedUnconfirmed`, `ErrNavigationFailed`, `ErrDebuggerDetached`, `ErrBusy`, or network resets) up to `3` times with full-jitter exponential backoff (`500ms` base, `8s` cap, honoring server `retryAfter`/`retryAfterMs` as a floor). Cancelled contexts never retry.
- **Manifest and exit contract:** Every attempt fsyncs one JSON line to `<out>/manifest.jsonl` (`index`, `url`, `title`, `bytes`, `file`, `waitedMs`, `ok`, `error`, `hash`, `simHash`). stdout receives only `FAIL NNN <url>: <reason>` lines and `OK: <scraped> scraped, <failed> failed[, <skipped> skipped] -> <out> (manifest.jsonl) [stopped: <reason>]`. Exits `1` if any item failed.
- **Equivalent shell loop** when custom per-page logic is needed (cap the list, stop on repeated hashes, keep jitter):

  ```bash
  while read -r url; do
    chrome-ctl open "$url" >/dev/null
    chrome-ctl wait --network --timeout 15000 >/dev/null
    chrome-ctl read > "pages/$(date +%s%N).md"
    chrome-ctl wait 3000-7000
  done < urls.txt
  ```

## Macros (`macro`)

Macros record successful CLI actions and replay them locally from `~/.chrome-ctl/macros/<name>.json` (mode `0600`) with zero LLM tokens.

```bash
chrome-ctl macro list
chrome-ctl macro start directory_search
chrome-ctl open "https://example.com/directory"
chrome-ctl snap
chrome-ctl type 1 "Software Agencies" --enter
chrome-ctl macro save --params search_term --description "Directory search"
chrome-ctl macro run directory_search --vars '{"search_term":"Design Studios"}'
```

- **Commands & flags:** `macro` has only `start`, `save`, `list`, and `run` (no `macro delete` or `--dry-run`; retire a macro by re-saving the same name or asking the user).
  - `macro start <name>` → `OK: recording macro "<name>"`.
  - `macro save [--params "a,b"] [--description "..."]` → `OK: saved macro "<name>" (N steps, params: a, b)`. `--params` takes comma-separated names bound sequentially to value-bearing steps (`open` → `url`, `type`/`upload` → `value`). Save only after verifying the flow worked.
  - `macro run <name> [--vars '{"k":"v"}'] [--run-id <id>] [--resume]` → 600s replay budget; outputs `OK: ran macro "<name>" (N steps, Xs[, resumed from step M])`. `--vars` accepts a JSON object of string values only.
- **Resilient selector resolution:** Before each recorded action mutates the page, the daemon captures a multi-tier `MacroTarget` descriptor (never snapshot IDs). Replay resolves tiers in order:
  1. `testId` (`[data-testid]` / `[data-qa]`)
  2. `role` + `ariaLabel`
  3. `text` (exact/normalized match)
  4. `xpath`
  5. `cssSelector`
- **Template expressions:** Macro JSON values support `{{name}}`, `{{trim name}}`, `{{lowercase name}}`, and `{{default name "fallback"}}`. Missing variables fail unless a default is given.
- **Saga checkpoints, circuit breakers & failure handling:**
  - Passing `--run-id <id>` writes append-only step checkpoints (`completed`, `skipped`, `failed`, `compensated`) to `<runID>.jsonl`; re-running with `--run-id <id> --resume` skips already-completed/skipped steps.
  - Replay classifies RPC errors (`transient`, `permanent`, `ambiguous` with effect-signature matching) and applies a per-step circuit breaker (`3` consecutive failures, `30s` cooldown).
  - On terminal failure, any completed steps with a `compensate` action execute in reverse order, a dead-letter JSON is written (`<runID>.deadletter.json`), stderr receives `MACRO STEP FAILED: macro "<name>" step <n>: <reason>` (plus `dead letter:`, `compensated:`, and a fresh `snap`), and the CLI exits `1` **silently** (no `Error:` prefix; never pipe stdout looking for failure detail).
  - **Repair rule:** Read the stderr snapshot, finish/repair the step with normal CLI commands, then re-record and `macro save` under the same name. Never rerun a broken macro unchanged.

## Resume

### What persists across interruptions

- **Automation window ID:** Stored in the extension's `chrome.storage.local`. Survives service-worker teardown, daemon restart, and extension reload. After a **Chrome restart**, the stored ID is stale; `window ensure` detects that, drops it, and creates a fresh window — always run `window ensure` first.
- **Macros:** `~/.chrome-ctl/macros/<name>.json` (mode `0600`).
- **`bulk` progress:** `<out>/manifest.jsonl` and `NNN-slug.md` files (stable index across resumes).
- **Network log:** Mirrors to session storage, so `net --since 5m` survives service-worker suspension, **not** a Chrome quit.
- **Media & logs:** `~/.chrome-ctl/media/` and `~/.chrome-ctl/daemon.log`.

### What does not persist

Snapshot IDs and the `SNAP` counter across navigation, reload, or a new tab; tab and window numbers across a Chrome restart; any page state; and **the ad-hoc task itself** (`chrome-ctl` keeps no session, checkpoint, or history for ad-hoc commands).

### Journal convention (`tasks/*.journal.jsonl`)

This is a documentation habit (no `chrome-ctl` command reads or writes it). Honor `CHROME_CTL_HOME` rather than hardcoding `~/.chrome-ctl` so alternate or containerized state directories work; the directory is mode `0700`.

```bash
JOURNAL="${CHROME_CTL_HOME:-$HOME/.chrome-ctl}/tasks/$(date +%Y%m%d)-signup.journal.jsonl"
mkdir -p "$(dirname "$JOURNAL")"
```

Append one JSON object per verified step:

```json
{"goal":"...","url":"...","snap":"SNAP 4 | TAB [81] ...","evidence":"Landed on button \"Connect\"","next":"type message","blocked":null}
```

### Resume recipes

1. **New agent session, work already started:** `status` → require `CONNECTED` → `window ensure` → `window list` → `url` → `snap`. Re-derive position from URL + fresh snapshot; never reuse an ID from the previous session. Read the journal's last line for intent.
2. **Interrupted `bulk`:** Inspect `[stopped: …]` from the previous summary first — a short page or 3 near-duplicates usually means a rate limit or login wall, and resuming without fixing it scrapes garbage. Then re-run the **exact same input list and `--out`** with `--resume`.
3. **Broken macro:** Follow the `MACRO STEP FAILED` row in [Recover](../SKILL.md#recover), repair manually, then re-record and `macro save` under the same name.
4. **Chrome restarted:** `window ensure` replaces the stale window ID; re-`open` from the journal's last URL. Check `window list` for `(minimized)` before any `shot` or `rec stop`.
5. **Daemon died:** `daemon --start` → `status` must show `CONNECTED` (extension re-pairs automatically) → `window ensure` → `snap`.
