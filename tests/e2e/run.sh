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
go build -o bin/chrome-ctl ./cmd/chrome-ctl

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

./bin/chrome-ctl daemon --stop
set +e
STOPPED="$(./bin/chrome-ctl status)"
RC=$?
set -e
echo "$STOPPED"
[[ "$RC" -eq 1 ]] || fail "status after stop exited $RC, want 1"
grep -q "Daemon: STOPPED" <<<"$STOPPED" || fail "daemon not reported stopped"

echo "Phase 1 E2E: PASS"
