package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func decodeWaitParams(t *testing.T, raw json.RawMessage) protocol.PageWaitParams {
	t.Helper()
	var params protocol.PageWaitParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("decode wait params: %v", err)
	}
	return params
}

func TestWaitCommandNetworkQuiet(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":1234,"matched":true,"requests":4}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "wait", "--network")
	if err != nil {
		t.Fatalf("wait --network error = %v", err)
	}
	if want := "OK: network quiet after 1.2s (4 requests)\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	request := <-requests
	if request.Method != protocol.MethodPageWait {
		t.Errorf("method = %q, want %q", request.Method, protocol.MethodPageWait)
	}
	params := decodeWaitParams(t, request.Params)
	if !params.Network || params.TimeoutMs != waitNetworkTimeoutMs || params.QuietMs != waitDefaultQuietMs {
		t.Errorf("params = %+v, want network with defaults", params)
	}
}

func TestWaitCommandNetworkBusyIsAWarning(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":15000,"matched":false,"reason":"timeout","inflight":3,"requests":9}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "wait", "--network")
	if err != nil {
		t.Fatalf("wait --network timeout error = %v, want nil", err)
	}
	if want := "WARN: network still busy after 15.0s (3 in flight, 9 requests seen). State unknown.\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestWaitCommandForTextPassesCaseFlag(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":300,"matched":true}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "wait", "--for-text", "Related keywords", "--ignore-case")
	if err != nil {
		t.Fatalf("wait --for-text error = %v", err)
	}
	if want := "OK: text \"Related keywords\" appeared after 300ms\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}

	params := decodeWaitParams(t, (<-requests).Params)
	if params.Text != "Related keywords" {
		t.Errorf("text = %q", params.Text)
	}
	if params.IgnoreCase == nil || !*params.IgnoreCase {
		t.Errorf("ignoreCase = %v, want true", params.IgnoreCase)
	}
}

func TestWaitCommandRejectsMixedPositionalAndFlags(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":0,"matched":true}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	if _, err := executePhase2(t, "wait", "idle", "--network"); err == nil {
		t.Fatal("wait idle --network error = nil, want rejection")
	}
}

func TestWaitCommandRequiresSomethingToWaitFor(t *testing.T) {
	_, err := executePhase2(t, "wait")
	if err == nil || !strings.Contains(err.Error(), "--network") {
		t.Fatalf("wait with no arguments error = %v, want guidance", err)
	}
}

func TestNetCommandPrintsTSV(t *testing.T) {
	_, token := setupStatusEnv(t)
	body := phase2Result(`{"entries":[
		{"url":"https://example.test/ok","method":"GET","status":200,"durationMs":42},
		{"url":"https://example.test/bad","method":"POST","status":500,"durationMs":120,"error":"net::ERR_FAILED"}
	]}`)
	srv := fakePhase2Server(t, token, body, nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "net", "--errors", "--since", "5m", "--limit", "10")
	if err != nil {
		t.Fatalf("net error = %v", err)
	}
	want := "200\tGET\t42ms\thttps://example.test/ok\n500\tPOST\t120ms\thttps://example.test/bad\tnet::ERR_FAILED\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestNetCommandEmptyLogExplainsWhy(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"entries":[],"note":"enable via an action or wait --network"}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "net")
	if err != nil {
		t.Fatalf("net error = %v", err)
	}
	if want := "OK: no requests recorded (enable via an action or wait --network)\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestNetCommandErrorsOnlySaysNoFailures(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"entries":[],"note":"enable via an action or wait --network"}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "net", "--errors")
	if err != nil {
		t.Fatalf("net --errors error = %v", err)
	}
	if want := "OK: no failed requests recorded (enable via an action or wait --network)\n"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestNetCommandRejectsInvalidSince(t *testing.T) {
	if _, err := parseSince("soon"); err == nil {
		t.Fatal("parseSince(soon) error = nil, want rejection")
	}
	if _, err := parseSince("-5m"); err == nil {
		t.Fatal("parseSince(-5m) error = nil, want rejection")
	}
	if ms, err := parseSince("5m"); err != nil || ms != 300000 {
		t.Fatalf("parseSince(5m) = %d, %v", ms, err)
	}
}
