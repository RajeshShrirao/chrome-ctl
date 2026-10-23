#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TMP="$(mktemp -d)"
export CHROME_CTL_HOME="$TMP/home"
export CHROME_CTL_ADDR="127.0.0.1:9329"
FAKE_PID=""

cleanup() {
  if [[ -n "$FAKE_PID" ]] && kill -0 "$FAKE_PID" 2>/dev/null; then
    kill "$FAKE_PID" 2>/dev/null || true
    wait "$FAKE_PID" 2>/dev/null || true
  fi
  ./bin/chrome-ctl daemon --stop >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  echo "--- extension log ---" >&2
  cat "$TMP/extension.log" >&2 2>/dev/null || true
  echo "--- daemon log ---" >&2
  cat "$CHROME_CTL_HOME/daemon.log" >&2 2>/dev/null || true
  exit 1
}

mkdir -p bin
go build -tags nodynamic -o bin/chrome-ctl ./cmd/chrome-ctl

START_OUT="$(./bin/chrome-ctl daemon --start)"
echo "$START_OUT"

TOKEN_FILE="$CHROME_CTL_HOME/auth_token"
[[ -f "$TOKEN_FILE" ]] || fail "token file missing"
PERMS="$(stat -f %Lp "$TOKEN_FILE" 2>/dev/null || stat -c %a "$TOKEN_FILE")"
[[ "$PERMS" == "600" ]] || fail "token permissions are $PERMS, want 600"
TOKEN="$(cat "$TOKEN_FILE")"
[[ ${#TOKEN} -eq 64 ]] || fail "token is ${#TOKEN} chars, want 64"

BASE="http://$CHROME_CTL_ADDR"
CODE="$(curl -s -o /dev/null -w '%{http_code}' "$BASE/health")"
[[ "$CODE" == "401" ]] || fail "unauthenticated /health returned $CODE, want 401"
CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$BASE/health")"
[[ "$CODE" == "200" ]] || fail "authenticated /health returned $CODE, want 200"
CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/rpc")"
[[ "$CODE" == "401" ]] || fail "unauthenticated /rpc returned $CODE, want 401"

node tests/e2e/fake_extension.mjs --addr "$CHROME_CTL_ADDR" --token "$TOKEN" --tabs 3 \
  >"$TMP/extension.log" 2>&1 &
FAKE_PID=$!

READY=0
for _ in $(seq 1 50); do
  if grep -q READY "$TMP/extension.log"; then
    READY=1
    break
  fi
  sleep 0.1
done
[[ "$READY" == "1" ]] || fail "fake extension did not become ready"

STATUS="$(./bin/chrome-ctl status)"
echo "$STATUS"
grep -q "Daemon: RUNNING ($CHROME_CTL_ADDR)" <<<"$STATUS" || fail "daemon not reported running"
grep -q "Chrome Extension: CONNECTED" <<<"$STATUS" || fail "extension not reported connected"
grep -q "Active Tabs: 3" <<<"$STATUS" || fail "tab count not reported"

TABS="$(./bin/chrome-ctl tabs)"
echo "$TABS"
grep -q "^\[1\] Fake Tab - https://example.test$" <<<"$TABS" || fail "tab 1 missing or malformed"
grep -q "^\[2\] Fake Tab 2 - https://example.test/2$" <<<"$TABS" || fail "tab 2 missing or malformed"

OPEN="$(./bin/chrome-ctl open "https://news.ycombinator.com")"
echo "$OPEN"
grep -q "^OK: navigated \[1\] Opened Page - https://news.ycombinator.com$" <<<"$OPEN" \
  || fail "open did not report navigation"

OPEN_NEW="$(./bin/chrome-ctl open "https://example.test/new" --new)"
echo "$OPEN_NEW"
grep -q "^OK: opened \[1\] Opened Page - https://example.test/new$" <<<"$OPEN_NEW" \
  || fail "open --new did not report a new tab"

SWITCH="$(./bin/chrome-ctl switch 2)"
echo "$SWITCH"
grep -q "^OK: switched to tab \[2\]$" <<<"$SWITCH" || fail "switch output malformed"

SNAP="$(./bin/chrome-ctl snap)"
echo "$SNAP"
grep -q "^TAB \[1\] Fake Tab | https://example.test$" <<<"$SNAP" || fail "snap header missing"
grep -q '^VIEWPORT: 0-900px of 4200px (21%)$' <<<"$SNAP" || fail "snap viewport line missing"
grep -q '^\[1\] <input:text> "Search" () @10,20$' <<<"$SNAP" || fail "snap input line malformed"
grep -q '^\[2\] <button> "Go" @40,60$' <<<"$SNAP" || fail "snap button line malformed"

FORMS="$(./bin/chrome-ctl snap --forms)"
echo "$FORMS"
grep -q '<input:text>' <<<"$FORMS" || fail "forms snap omitted the input"
if grep -q '<button>' <<<"$FORMS"; then
  fail "forms snap included a button"
fi

READ="$(./bin/chrome-ctl read)"
grep -q "# Fake Heading" <<<"$READ" || fail "read did not print markdown"
grep -q "Requested: page" <<<"$READ" || fail "read did not target the page"
grep -q "Requested: 2" <<<"$(./bin/chrome-ctl read 2)" || fail "read by id failed"
grep -q "Requested: main" <<<"$(./bin/chrome-ctl read main)" || fail "read by selector failed"

# Phase 3: humanized actions
CLICK="$(./bin/chrome-ctl click 2 2>&1)" || fail "click 2 failed"
echo "$CLICK"
grep -q 'OK: clicked \[2\] <button> "Go" @40,60 (0.34s)' <<<"$CLICK" \
  || fail "click output malformed"

CLICK_SNAP="$(./bin/chrome-ctl click 2 --snap 2>&1)" || fail "click 2 --snap failed"
echo "$CLICK_SNAP"
grep -q 'OK: clicked \[2\] <button> "Go" @40,60 (0.34s)' <<<"$CLICK_SNAP" \
  || fail "click --snap omitted the click summary"
grep -q '^TAB \[1\] Fake Tab | https://example.test$' <<<"$CLICK_SNAP" \
  || fail "click --snap omitted the snapshot header"
grep -q '^\[1\] <input:text> "Search" () @10,20$' <<<"$CLICK_SNAP" \
  || fail "click --snap omitted the snapshot element"

CLICK_DOUBLE="$(./bin/chrome-ctl click 2 --double 2>&1)" || fail "click 2 --double failed"
echo "$CLICK_DOUBLE"
grep -q 'OK: double-clicked \[2\] <button> "Go" @40,60 (0.34s)' <<<"$CLICK_DOUBLE" \
  || fail "double-click output malformed"

TYPE_TEXT="$(./bin/chrome-ctl type 3 "Hello" 2>&1)" || fail "type 3 Hello failed"
echo "$TYPE_TEXT"
grep -q 'OK: Typed 5 chars into \[3\] (human cadence: 1.2s, 1 typo). State changed: \[6\] <button:submit> "Send" is now (enabled).' <<<"$TYPE_TEXT" \
  || fail "type output malformed"

TYPE_ENTER="$(./bin/chrome-ctl type 3 "Hi" --enter 2>&1)" || fail "type 3 Hi --enter failed"
echo "$TYPE_ENTER"
grep -q 'OK: Typed 2 chars into \[3\] (human cadence: 1.2s, 1 typo).' <<<"$TYPE_ENTER" \
  || fail "type --enter output malformed"

PRESS="$(./bin/chrome-ctl press Enter 2>&1)" || fail "press Enter failed"
echo "$PRESS"
[[ "$PRESS" == "OK: pressed Enter" ]] || fail "press Enter output malformed: $PRESS"

PRESS_COMBO="$(./bin/chrome-ctl press Tab --modifier ctrl --modifier shift 2>&1)" \
  || fail "press Tab with modifiers failed"
echo "$PRESS_COMBO"
grep -q 'OK: pressed Ctrl+Shift+Tab' <<<"$PRESS_COMBO" \
  || fail "press with modifiers output malformed"

SCROLL_DOWN="$(./bin/chrome-ctl scroll down 2>&1)" || fail "scroll down failed"
echo "$SCROLL_DOWN"
grep -q 'OK: scrolled down 720px to 720px (17%)' <<<"$SCROLL_DOWN" \
  || fail "scroll down output malformed"

SCROLL_UP="$(./bin/chrome-ctl scroll up 0.5 2>&1)" || fail "scroll up 0.5 failed"
echo "$SCROLL_UP"
grep -q 'OK: scrolled up 720px to 720px (17%)' <<<"$SCROLL_UP" \
  || fail "scroll up output malformed"

printf 'hello' > "$TMP/upload.txt"
UPLOAD="$(./bin/chrome-ctl upload 5 "$TMP/upload.txt" 2>&1)" || fail "upload failed"
echo "$UPLOAD"
[[ "$UPLOAD" == "OK: uploaded upload.txt (5 B) to [5]" ]] \
  || fail "upload output malformed: $UPLOAD"

WAIT_MS="$(./bin/chrome-ctl wait 50 2>&1)" || fail "wait 50 failed"
echo "$WAIT_MS"
[[ "$WAIT_MS" == "OK: waited 50ms" ]] || fail "wait ms output malformed: $WAIT_MS"

WAIT_IDLE="$(./bin/chrome-ctl wait idle 2>&1)" || fail "wait idle failed"
echo "$WAIT_IDLE"
[[ "$WAIT_IDLE" == "OK: idle after 1.2s" ]] || fail "wait idle output malformed: $WAIT_IDLE"

WAIT_SELECTOR="$(./bin/chrome-ctl wait "#results" 2>&1)" || fail "wait selector failed"
echo "$WAIT_SELECTOR"
[[ "$WAIT_SELECTOR" == 'OK: selector "#results" appeared after 1.2s' ]] \
  || fail "wait selector output malformed: $WAIT_SELECTOR"

# Phase 4: vision and media
SHOT="$(./bin/chrome-ctl shot --som -o "$TMP/som.webp" 2>&1)" || fail "shot --som failed"
echo "$SHOT"
grep -q "OK: saved $TMP/som.webp (320x180, " <<<"$SHOT" || fail "shot output malformed"
grep -q ", 2 badges)" <<<"$SHOT" || fail "shot did not report the Set-of-Mark badges"
[[ -s "$TMP/som.webp" ]] || fail "shot wrote an empty file"
MAGIC="$(xxd -p -l 12 "$TMP/som.webp")"
[[ "$MAGIC" == 52494646*57454250 ]] || fail "shot is not a WebP (magic $MAGIC)"

SHOT_GRAY="$(./bin/chrome-ctl shot --gray -o "$TMP/gray.webp" 2>&1)" || fail "shot --gray failed"
echo "$SHOT_GRAY"
grep -q ", grayscale)" <<<"$SHOT_GRAY" || fail "shot --gray output malformed"
[[ -s "$TMP/gray.webp" ]] || fail "grayscale shot wrote an empty file"

REC_START="$(./bin/chrome-ctl rec start --fps 2 2>&1)" || fail "rec start failed"
echo "$REC_START"
[[ "$REC_START" == "OK: recording at 2fps" ]] || fail "rec start output malformed: $REC_START"
sleep 1.2
REC_STOP="$(./bin/chrome-ctl rec stop --grid -o "$TMP/storyboard.webp" 2>&1)" || fail "rec stop --grid failed"
echo "$REC_STOP"
grep -q "OK: saved $TMP/storyboard.webp (" <<<"$REC_STOP" || fail "rec stop output malformed"
grep -q ", 3x4 grid, " <<<"$REC_STOP" || fail "rec stop --grid did not report the grid layout"
[[ -s "$TMP/storyboard.webp" ]] || fail "storyboard is empty"
MAGIC="$(xxd -p -l 12 "$TMP/storyboard.webp")"
[[ "$MAGIC" == 52494646*57454250 ]] || fail "storyboard is not a WebP (magic $MAGIC)"

if command -v ffmpeg >/dev/null 2>&1; then
  ./bin/chrome-ctl rec start >/dev/null || fail "rec start for WebM failed"
  sleep 0.8
  REC_WEBM="$(./bin/chrome-ctl rec stop -o "$TMP/clip.webm" 2>&1)" || fail "rec stop WebM failed"
  echo "$REC_WEBM"
  grep -q "OK: saved $TMP/clip.webm (" <<<"$REC_WEBM" || fail "WebM stop output malformed"
  [[ -s "$TMP/clip.webm" ]] || fail "WebM file is empty"
  MAGIC="$(xxd -p -l 4 "$TMP/clip.webm")"
  [[ "$MAGIC" == "1a45dfa3" ]] || fail "WebM file has no EBML header (magic $MAGIC)"
fi

# Phase 5: SSO and local macro replay
SSO_OUT="$(./bin/chrome-ctl sso --account fake@example.test 2>&1)" || fail "sso failed"
echo "$SSO_OUT"
[[ "$SSO_OUT" == "OK: signed in as fake@example.test (redirect complete)" ]] \
  || fail "sso output malformed: $SSO_OUT"

set +e
SSO_TIMEOUT="$(./bin/chrome-ctl sso --account timeout@example.test --timeout 1000 2>&1)"
RC=$?
set -e
echo "$SSO_TIMEOUT"
[[ "$RC" -eq 1 ]] || fail "sso timeout exited $RC, want 1"
grep -q "SSO timeout" <<<"$SSO_TIMEOUT" || fail "sso timeout did not report the timeout"

MACRO_START="$(./bin/chrome-ctl macro start lead_search 2>&1)" || fail "macro start failed"
echo "$MACRO_START"
[[ "$MACRO_START" == 'OK: recording macro "lead_search"' ]] || fail "macro start output malformed"

./bin/chrome-ctl open "https://example.test/directory" >/dev/null || fail "macro recording open failed"
./bin/chrome-ctl type 1 "Software Agencies" >/dev/null || fail "macro recording type failed"

MACRO_SAVE="$(./bin/chrome-ctl macro save --params search_term --description "Directory search" 2>&1)" \
  || fail "macro save failed"
echo "$MACRO_SAVE"
grep -q '^OK: saved macro "lead_search" (2 steps, params: search_term)$' <<<"$MACRO_SAVE" \
  || fail "macro save output malformed: $MACRO_SAVE"

MACRO_FILE="$CHROME_CTL_HOME/macros/lead_search.json"
[[ -f "$MACRO_FILE" ]] || fail "macro file missing"
PERMS="$(stat -f %Lp "$MACRO_FILE" 2>/dev/null || stat -c %a "$MACRO_FILE")"
[[ "$PERMS" == "600" ]] || fail "macro file permissions are $PERMS, want 600"
grep -q '"{{search_term}}"' "$MACRO_FILE" || fail "macro file was not parameterized"
grep -q '"macro_version": "1.0"' "$MACRO_FILE" || fail "macro file has no version"

MACRO_LIST="$(./bin/chrome-ctl macro list 2>&1)" || fail "macro list failed"
echo "$MACRO_LIST"
grep -q "^lead_search - 2 steps \[params: search_term\] - Directory search$" <<<"$MACRO_LIST" \
  || fail "macro list output malformed: $MACRO_LIST"

MACRO_RUN="$(./bin/chrome-ctl macro run lead_search --vars '{"search_term":"Design Studios"}' 2>&1)" \
  || fail "macro run failed"
echo "$MACRO_RUN"
grep -q '^OK: ran macro "lead_search" (2 steps, ' <<<"$MACRO_RUN" || fail "macro run output malformed"

set +e
MACRO_MISSING="$(./bin/chrome-ctl macro run lead_search 2>&1)"
RC=$?
set -e
echo "$MACRO_MISSING"
[[ "$RC" -eq 1 ]] || fail "macro run without vars exited $RC, want 1"
grep -q 'missing variable' <<<"$MACRO_MISSING" || fail "macro run did not reject the missing variable"

kill "$FAKE_PID" 2>/dev/null || true
wait "$FAKE_PID" 2>/dev/null || true
FAKE_PID=""
sleep 0.5
set +e
NOEXT="$(./bin/chrome-ctl snap 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 1 ]] || fail "snap without an extension exited $RC, want 1"
grep -qi "extension not connected" <<<"$NOEXT" || fail "snap without an extension did not report the connection error"
set +e
NOCLICK="$(./bin/chrome-ctl click 2 2>&1)"
RC=$?
set -e
[[ "$RC" -eq 1 ]] || fail "click without an extension exited $RC, want 1"
grep -qi "extension not connected" <<<"$NOCLICK" || fail "click without an extension did not report the connection error"

./bin/chrome-ctl daemon --stop
set +e
STOPPED="$(./bin/chrome-ctl status)"
RC=$?
set -e
echo "$STOPPED"
[[ "$RC" -eq 1 ]] || fail "status after stop exited $RC, want 1"
grep -q "Daemon: STOPPED" <<<"$STOPPED" || fail "daemon not reported stopped"

echo "E2E: PASS"
