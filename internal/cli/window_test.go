package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func fakeWindowServer(t *testing.T, token string, responses map[string]string, requests chan<- phase2Request) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q, want %q", got, "Bearer "+token)
		}
		var req phase2Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if requests != nil {
			requests <- req
		}
		body, ok := responses[req.Method]
		if !ok {
			t.Errorf("unexpected method %q", req.Method)
			body = phase2Error(-32601, "method not found")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
}

func TestFormatWindows(t *testing.T) {
	windows := []protocol.WindowInfo{
		{WindowID: 12, Focused: true, State: "normal", TabCount: 2},
		{WindowID: 34, State: "minimized", TabCount: 1, Automation: true},
		{WindowID: 56, State: "normal", TabCount: 4, Automation: true},
	}
	want := "* [12] 2 tabs\n" +
		"  [34] 1 tab (minimized) (automation)\n" +
		"  [56] 4 tabs (automation)\n"
	if got := formatWindows(windows); got != want {
		t.Errorf("formatWindows() =\n%q\nwant\n%q", got, want)
	}
	if got := formatWindows(nil); got != "No open windows.\n" {
		t.Errorf("formatWindows(nil) = %q", got)
	}
}

func TestWindowEnsureAndNew(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 4)
	srv := fakeWindowServer(t, token, map[string]string{
		protocol.MethodWindowEnsure: phase2Result(`{"windowId":12,"activeTabId":3,"tabCount":1,"focused":false,"created":true}`),
		protocol.MethodWindowCreate: phase2Result(`{"windowId":13,"activeTabId":4,"tabCount":2,"focused":true,"created":true}`),
	}, requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "window", "ensure")
	if err != nil {
		t.Fatalf("window ensure: %v", err)
	}
	want := "OK: created automation window [12] (tab [3], 1 tab, unfocused)\n"
	if out != want {
		t.Errorf("window ensure output = %q, want %q", out, want)
	}
	req := <-requests
	if req.Method != protocol.MethodWindowEnsure || len(req.Params) != 0 {
		t.Errorf("ensure request = %s %s", req.Method, req.Params)
	}

	out, err = executePhase2(t, "window", "new", "https://example.test", "--foreground")
	if err != nil {
		t.Fatalf("window new: %v", err)
	}
	want = "OK: created automation window [13] (tab [4], 2 tabs, focused)\n"
	if out != want {
		t.Errorf("window new output = %q, want %q", out, want)
	}
	req = <-requests
	if req.Method != protocol.MethodWindowCreate {
		t.Errorf("new method = %q", req.Method)
	}
	if string(req.Params) != `{"url":"https://example.test","focused":true}` {
		t.Errorf("new params = %s", req.Params)
	}
}

func TestWindowEnsureReusesWindow(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakeWindowServer(t, token, map[string]string{
		protocol.MethodWindowEnsure: phase2Result(`{"windowId":12,"activeTabId":3,"tabCount":3,"focused":false,"created":false}`),
	}, nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "window", "ensure")
	if err != nil {
		t.Fatalf("window ensure: %v", err)
	}
	want := "OK: using automation window [12] (tab [3], 3 tabs, unfocused)\n"
	if out != want {
		t.Errorf("window ensure output = %q, want %q", out, want)
	}
}

func TestWindowUseAndClear(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 4)
	srv := fakeWindowServer(t, token, map[string]string{
		protocol.MethodWindowUse: phase2Result(`{"windowId":44,"activeTabId":9,"tabCount":2,"focused":true}`),
	}, requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "window", "use", "focused")
	if err != nil {
		t.Fatalf("window use focused: %v", err)
	}
	want := "OK: automation window is now [44] (tab [9], 2 tabs, focused)\n"
	if out != want {
		t.Errorf("window use output = %q, want %q", out, want)
	}
	if req := <-requests; string(req.Params) != `{"focused":true}` {
		t.Errorf("window use params = %s", req.Params)
	}

	if _, err := executePhase2(t, "window", "use", "44"); err != nil {
		t.Fatalf("window use 44: %v", err)
	}
	if req := <-requests; string(req.Params) != `{"windowId":44}` {
		t.Errorf("window use id params = %s", req.Params)
	}

	if _, err := executePhase2(t, "window", "use", "not-a-number"); err == nil {
		t.Error("window use with a non-numeric id should fail")
	}

	out, err = executePhase2(t, "window", "clear")
	if err != nil {
		t.Fatalf("window clear: %v", err)
	}
	if !strings.HasPrefix(out, "OK: automation window cleared") {
		t.Errorf("window clear output = %q", out)
	}
	if req := <-requests; len(req.Params) != 0 {
		t.Errorf("window clear params = %s, want omitted", req.Params)
	}
}

func TestWindowCloseRefusesFocusedWindow(t *testing.T) {
	_, token := setupStatusEnv(t)
	list := phase2Result(`{"windows":[{"windowId":44,"focused":true,"state":"normal","tabCount":2,"automation":false}]}`)
	requests := make(chan phase2Request, 4)
	srv := fakeWindowServer(t, token, map[string]string{
		protocol.MethodWindowList:  list,
		protocol.MethodWindowClose: phase2Result(`{"success":true,"windowId":44}`),
	}, requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	_, err := executePhase2(t, "window", "close", "44")
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("close on a focused window = %v, want a --force hint", err)
	}
	if req := <-requests; req.Method != protocol.MethodWindowList {
		t.Errorf("first request = %q, want window.list", req.Method)
	}
	if len(requests) != 0 {
		t.Errorf("close should not be sent without --force")
	}

	out, err := executePhase2(t, "window", "close", "44", "--force")
	if err != nil {
		t.Fatalf("close --force: %v", err)
	}
	if out != "OK: closed window [44]\n" {
		t.Errorf("close --force output = %q", out)
	}
	if req := <-requests; req.Method != protocol.MethodWindowClose {
		t.Errorf("forced request = %q, want window.close", req.Method)
	}
}

func TestWindowCloseUnfocusedWindow(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 4)
	srv := fakeWindowServer(t, token, map[string]string{
		protocol.MethodWindowList: phase2Result(
			`{"windows":[{"windowId":44,"focused":false,"state":"normal","tabCount":1,"automation":true}]}`,
		),
		protocol.MethodWindowClose: phase2Result(`{"success":true,"windowId":44}`),
	}, requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "window", "close", "44")
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if out != "OK: closed window [44]\n" {
		t.Errorf("close output = %q", out)
	}
	<-requests
	req := <-requests
	if string(req.Params) != `{"windowId":44}` {
		t.Errorf("close params = %s", req.Params)
	}
}
