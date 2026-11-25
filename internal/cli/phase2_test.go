package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

type phase2Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func fakePhase2Server(t *testing.T, token, body string, requests chan<- phase2Request) *httptest.Server {
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
}

func phase2Result(result string) string {
	return `{"jsonrpc":"2.0","id":"x","result":` + result + `}`
}

func phase2Error(code int, message string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":"x","error":{"code":%d,"message":%q}}`, code, message)
}

func executePhase2(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	root.SetArgs(args)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	err := root.Execute()
	return out.String(), err
}

func TestFormatTabs(t *testing.T) {
	tests := []struct {
		name string
		tabs []protocol.TabInfo
		want string
	}{
		{
			name: "empty",
			tabs: nil,
			want: "No open tabs.\n",
		},
		{
			name: "empty slice",
			tabs: []protocol.TabInfo{},
			want: "No open tabs.\n",
		},
		{
			name: "document order with fallbacks",
			tabs: []protocol.TabInfo{
				{TabID: 1, Title: "Example", URL: "https://example.test", Active: true},
				{TabID: 2, Title: "", URL: ""},
				{TabID: 3, Title: "Only title"},
			},
			want: "[1] Example - https://example.test\n[2] (untitled) - (no url)\n[3] Only title - (no url)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatTabs(tt.tabs); got != tt.want {
				t.Errorf("formatTabs() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestFormatOpen(t *testing.T) {
	tests := []struct {
		name   string
		result protocol.TabOpenResult
		newTab bool
		want   string
	}{
		{
			name:   "navigate",
			result: protocol.TabOpenResult{TabID: 4, URL: "https://example.test", Title: "Example", Loaded: true},
			want:   "OK: navigated [4] Example - https://example.test\n",
		},
		{
			name:   "new tab",
			result: protocol.TabOpenResult{TabID: 7, URL: "https://example.test/new", Title: "New", Loaded: true},
			newTab: true,
			want:   "OK: opened [7] New - https://example.test/new\n",
		},
		{
			name:   "empty title",
			result: protocol.TabOpenResult{TabID: 2, URL: "https://example.test/x", Title: "", Loaded: true},
			want:   "OK: navigated [2] (untitled) - https://example.test/x\n",
		},
		{
			name:   "still loading",
			result: protocol.TabOpenResult{TabID: 3, URL: "https://example.test/slow", Title: "Slow", Loaded: false},
			newTab: true,
			want:   "OK: opened [3] Slow - https://example.test/slow (still loading)\n",
		},
		{
			name: "wait matched with requests",
			result: protocol.TabOpenResult{
				TabID: 4, URL: "https://example.test", Title: "Example", Loaded: true,
				Wait: &protocol.PageWaitResult{WaitedMs: 1200, Matched: true, Requests: 4},
			},
			want: "OK: navigated [4] Example - https://example.test\nOK: wait settled after 1.2s (4 requests)\n",
		},
		{
			name: "wait matched without requests",
			result: protocol.TabOpenResult{
				TabID: 4, URL: "https://example.test", Title: "Example", Loaded: true,
				Wait: &protocol.PageWaitResult{WaitedMs: 300, Matched: true},
			},
			want: "OK: navigated [4] Example - https://example.test\nOK: wait settled after 300ms\n",
		},
		{
			name: "wait timed out with snapshot",
			result: protocol.TabOpenResult{
				TabID: 4, URL: "https://example.test", Title: "Example", Loaded: true,
				Wait:     &protocol.PageWaitResult{WaitedMs: 5000, Matched: false, Reason: "timeout"},
				Snapshot: &protocol.SnapResult{FormattedText: "SNAP 1 | TAB [4] Example | https://example.test\n\n"},
			},
			want: "OK: navigated [4] Example - https://example.test\nWARN: open wait timed out after 5.0s; state may still be settling.\n\nSNAP 1 | TAB [4] Example | https://example.test\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatOpen(tt.result, tt.newTab); got != tt.want {
				t.Errorf("formatOpen() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadParams(t *testing.T) {
	noArg, err := readParams(nil)
	if err != nil {
		t.Fatalf("readParams(nil) error = %v", err)
	}
	if noArg.ID != nil || noArg.TabID != nil || noArg.Selector != "" {
		t.Errorf("readParams(nil) = %+v, want empty params", noArg)
	}

	byID, err := readParams([]string{"42"})
	if err != nil {
		t.Fatalf("readParams(id) error = %v", err)
	}
	if byID.ID == nil || *byID.ID != 42 {
		t.Errorf("readParams(id).ID = %v, want 42", byID.ID)
	}
	if byID.Selector != "" {
		t.Errorf("readParams(id).Selector = %q, want empty", byID.Selector)
	}

	bySelector, err := readParams([]string{"main .content"})
	if err != nil {
		t.Fatalf("readParams(selector) error = %v", err)
	}
	if bySelector.ID != nil {
		t.Errorf("readParams(selector).ID = %v, want nil", bySelector.ID)
	}
	if bySelector.Selector != "main .content" {
		t.Errorf("readParams(selector).Selector = %q, want %q", bySelector.Selector, "main .content")
	}

	if _, err := readParams([]string{strings.Repeat("9", 40)}); err == nil {
		t.Error("readParams(overflowing id) error = nil, want error")
	}
}

func TestTabsCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(
		`{"tabs":[{"tabId":1,"title":"Example","url":"https://example.test","active":true},`+
			`{"tabId":2,"title":"","url":"","active":false}]}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "tabs")
	if err != nil {
		t.Fatalf("tabs error = %v", err)
	}
	want := "[1] Example - https://example.test\n[2] (untitled) - (no url)\n"
	if out != want {
		t.Errorf("tabs output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodTabList {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodTabList)
	}
	if len(req.Params) != 0 {
		t.Errorf("params = %s, want omitted", req.Params)
	}
}

func TestTabsCommandEmpty(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"tabs":[]}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "tabs")
	if err != nil {
		t.Fatalf("tabs error = %v", err)
	}
	if out != "No open tabs.\n" {
		t.Errorf("tabs output = %q, want %q", out, "No open tabs.\n")
	}
}

func TestOpenCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 2)
	srv := fakePhase2Server(t, token, phase2Result(
		`{"tabId":7,"url":"https://example.test","title":"Example","loaded":true}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "open", " https://example.test ")
	if err != nil {
		t.Fatalf("open error = %v", err)
	}
	if want := "OK: navigated [7] Example - https://example.test\n"; out != want {
		t.Errorf("open output = %q, want %q", out, want)
	}
	req := <-requests
	if req.Method != protocol.MethodTabOpen {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodTabOpen)
	}
	var params protocol.TabOpenParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.URL != "https://example.test" || params.NewTab || params.WaitNetwork || params.WaitText != "" || params.WaitIgnoreCase || params.WaitTimeoutMs != 0 || params.Snap {
		t.Errorf("params = %+v, want trimmed URL and zero wait/snap flags", params)
	}

	out, err = executePhase2(t, "open", "https://example.test", "--new")
	if err != nil {
		t.Fatalf("open --new error = %v", err)
	}
	if want := "OK: opened [7] Example - https://example.test\n"; out != want {
		t.Errorf("open --new output = %q, want %q", out, want)
	}
	req = <-requests
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if !params.NewTab {
		t.Error("params.NewTab = false, want true")
	}
}

func TestOpenCommandWithWaitAndSnap(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 3)

	t.Run("matched wait and snapshot", func(t *testing.T) {
		srv := fakePhase2Server(t, token, phase2Result(
			`{"tabId":7,"url":"https://example.test","title":"Example","loaded":true,`+
				`"wait":{"waitedMs":1200,"matched":true,"requests":4},`+
				`"snapshot":{"formattedText":"SNAP 1 | TAB [7] Example | https://example.test\n[1] <button> \"Done\"\n\n"}}`), requests)
		defer srv.Close()
		t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

		out, err := executePhase2(t, "open", "https://example.test", "--wait", "--for-text", "Done", "--ignore-case", "--timeout", "5000", "--snap")
		if err != nil {
			t.Fatalf("open --wait --snap error = %v", err)
		}
		want := "OK: navigated [7] Example - https://example.test\n" +
			"OK: wait settled after 1.2s (4 requests)\n\n" +
			"SNAP 1 | TAB [7] Example | https://example.test\n" +
			"[1] <button> \"Done\"\n"
		if out != want {
			t.Errorf("output =\n%q\nwant\n%q", out, want)
		}

		req := <-requests
		if req.Method != protocol.MethodTabOpen {
			t.Errorf("method = %q, want %q", req.Method, protocol.MethodTabOpen)
		}
		var params protocol.TabOpenParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if !params.WaitNetwork || params.WaitText != "Done" || !params.WaitIgnoreCase || params.WaitTimeoutMs != 5000 || !params.Snap {
			t.Errorf("params = %+v, want WaitNetwork, WaitText=Done, WaitIgnoreCase, WaitTimeoutMs=5000, Snap", params)
		}
	})

	t.Run("timed out wait WARN and snapshot", func(t *testing.T) {
		srv := fakePhase2Server(t, token, phase2Result(
			`{"tabId":7,"url":"https://example.test","title":"Example","loaded":true,`+
				`"wait":{"waitedMs":5000,"matched":false,"reason":"timeout","inflight":1,"requests":3},`+
				`"snapshot":{"formattedText":"SNAP 1 | TAB [7] Example | https://example.test\n"}}`), requests)
		defer srv.Close()
		t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

		out, err := executePhase2(t, "open", "https://example.test", "--wait", "--for-text", "Done", "--ignore-case", "--timeout", "5000", "--snap")
		if err != nil {
			t.Fatalf("open --wait timeout error = %v", err)
		}
		want := "OK: navigated [7] Example - https://example.test\n" +
			"WARN: open wait timed out after 5.0s (1 in flight, 3 requests); state may still be settling.\n\n" +
			"SNAP 1 | TAB [7] Example | https://example.test\n"
		if out != want {
			t.Errorf("output =\n%q\nwant\n%q", out, want)
		}
		<-requests
	})

	t.Run("wait uses default 15000ms timeout", func(t *testing.T) {
		srv := fakePhase2Server(t, token, phase2Result(
			`{"tabId":7,"url":"https://example.test","title":"Example","loaded":true,"wait":{"waitedMs":200,"matched":true}}`), requests)
		defer srv.Close()
		t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

		out, err := executePhase2(t, "open", "https://example.test", "--wait")
		if err != nil {
			t.Fatalf("open --wait error = %v", err)
		}
		want := "OK: navigated [7] Example - https://example.test\nOK: wait settled after 200ms\n"
		if out != want {
			t.Errorf("output = %q, want %q", out, want)
		}
		var params protocol.TabOpenParams
		if err := json.Unmarshal((<-requests).Params, &params); err != nil {
			t.Fatalf("decode params: %v", err)
		}
		if !params.WaitNetwork || params.WaitTimeoutMs != 15000 || params.WaitIgnoreCase || params.Snap {
			t.Errorf("params = %+v, want WaitNetwork and WaitTimeoutMs=15000", params)
		}
	})
}

func TestOpenCommandValidationAndTimeoutBudget(t *testing.T) {
	setupStatusEnv(t)
	for _, args := range [][]string{
		{"open", "https://example.test", "--ignore-case"},
		{"open", "https://example.test", "--for-text", "   "},
		{"open", "https://example.test", "--wait", "--timeout", "0"},
		{"open", "https://example.test", "--wait", "--timeout", "30001"},
	} {
		if _, err := executePhase2(t, args...); err == nil {
			t.Errorf("%v error = nil, want validation error", args)
		}
	}

	if d, ok := openRPCTimeout(false, false, 15000); ok || d != 0 {
		t.Errorf("openRPCTimeout(false, false, 15000) = %v, %v; want 0, false", d, ok)
	}
	if d, ok := openRPCTimeout(true, true, 5000); !ok || d != 20*time.Second {
		t.Errorf("openRPCTimeout(true, true, 5000) = %v, %v; want 20s, true", d, ok)
	}
	if d, ok := openRPCTimeout(true, false, 30000); !ok || d != 45*time.Second {
		t.Errorf("openRPCTimeout(true, false, 30000) = %v, %v; want 45s, true", d, ok)
	}
	if d, ok := openRPCTimeout(false, true, 15000); !ok || d != 30*time.Second {
		t.Errorf("openRPCTimeout(false, true, 15000) = %v, %v; want 30s, true", d, ok)
	}
}

func TestOpenCommandRejectsBlankURL(t *testing.T) {
	setupStatusEnv(t)
	if _, err := executePhase2(t, "open", "   "); err == nil {
		t.Fatal("open blank URL error = nil, want error")
	}
}

func TestSwitchCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(`{"success":true,"activeTabId":2}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "switch", "2")
	if err != nil {
		t.Fatalf("switch error = %v", err)
	}
	if want := "OK: switched to tab [2]\n"; out != want {
		t.Errorf("switch output = %q, want %q", out, want)
	}

	req := <-requests
	var params protocol.TabSwitchParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.TabID != 2 {
		t.Errorf("params.TabID = %d, want 2", params.TabID)
	}
}

func TestSwitchCommandFailure(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"success":false,"activeTabId":1}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	if _, err := executePhase2(t, "switch", "9"); err == nil {
		t.Fatal("switch failure error = nil, want error")
	}
}

func TestSwitchCommandRejectsInvalidID(t *testing.T) {
	setupStatusEnv(t)
	for _, arg := range []string{"0", "-1", "abc"} {
		if _, err := executePhase2(t, "switch", arg); err == nil {
			t.Errorf("switch %q error = nil, want error", arg)
		}
	}
}

func TestSnapCommandSingleNewline(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token,
		phase2Result(`{"formattedText":"line1\nline2\n\n"}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "snap", "--all", "--forms")
	if err != nil {
		t.Fatalf("snap error = %v", err)
	}
	if want := "line1\nline2\n"; out != want {
		t.Errorf("snap output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodPageSnap {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodPageSnap)
	}
	var params protocol.PageSnapParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.TabID != nil || !params.All || !params.FormsOnly || params.Diff {
		t.Errorf("params = %+v, want TabID nil, All and FormsOnly true, Diff false", params)
	}
}

func TestSnapCommandDiff(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token,
		phase2Result(`{"formattedText":"DIFF 1->2 | TAB [1] Example | https://example.test\n+ [2] <button> \"New\"\n\n"}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "snap", "--diff")
	if err != nil {
		t.Fatalf("snap --diff error = %v", err)
	}
	want := "DIFF 1->2 | TAB [1] Example | https://example.test\n+ [2] <button> \"New\"\n"
	if out != want {
		t.Errorf("snap --diff output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodPageSnap {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodPageSnap)
	}
	var params protocol.PageSnapParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if !params.Diff {
		t.Errorf("params.Diff = false, want true (%+v)", params)
	}
}

func TestReadCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 3)
	srv := fakePhase2Server(t, token,
		phase2Result(`{"markdown":"# Heading\n\nBody text\n\n\n"}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "read")
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	if want := "# Heading\n\nBody text\n"; out != want {
		t.Errorf("read output = %q, want %q", out, want)
	}
	page := <-requests
	var pageParams protocol.PageReadParams
	if err := json.Unmarshal(page.Params, &pageParams); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if pageParams.ID != nil || pageParams.TabID != nil || pageParams.Selector != "" {
		t.Errorf("read params = %+v, want empty", pageParams)
	}

	if _, err := executePhase2(t, "read", "2"); err != nil {
		t.Fatalf("read 2 error = %v", err)
	}
	byID := <-requests
	var idParams protocol.PageReadParams
	if err := json.Unmarshal(byID.Params, &idParams); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if idParams.ID == nil || *idParams.ID != 2 || idParams.Selector != "" {
		t.Errorf("read 2 params = %+v, want ID 2", idParams)
	}

	if _, err := executePhase2(t, "read", "main"); err != nil {
		t.Fatalf("read main error = %v", err)
	}
	bySelector := <-requests
	var selectorParams protocol.PageReadParams
	if err := json.Unmarshal(bySelector.Params, &selectorParams); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if selectorParams.ID != nil || selectorParams.Selector != "main" {
		t.Errorf("read main params = %+v, want selector main", selectorParams)
	}
}

func TestMapRPCError(t *testing.T) {
	err := mapRPCError(&protocol.RPCError{Code: protocol.ErrConnection, Message: "extension offline"})
	if err == nil || !strings.Contains(err.Error(), "Chrome extension not connected") {
		t.Errorf("mapRPCError(-32000) = %v, want extension not connected", err)
	}

	err = mapRPCError(&protocol.RPCError{Code: -32001, Message: "element not found"})
	if err == nil || err.Error() != "element not found" {
		t.Errorf("mapRPCError(-32001) = %v, want %q", err, "element not found")
	}
}

func TestConnectionErrorOnCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Error(protocol.ErrConnection, "extension offline"), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	_, err := executePhase2(t, "snap")
	if err == nil {
		t.Fatal("snap error = nil, want extension connection error")
	}
	if !strings.Contains(err.Error(), "Chrome extension not connected") {
		t.Errorf("snap error = %q, want extension not connected", err)
	}
}

func TestExecuteExitPathOnConnectionError(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Error(protocol.ErrConnection, "extension offline"), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	oldArgs := os.Args
	os.Args = []string{"chrome-ctl", "snap"}
	t.Cleanup(func() { os.Args = oldArgs })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	code := Execute()
	_ = w.Close()
	os.Stderr = oldStderr
	stderr, _ := io.ReadAll(r)

	if code != 1 {
		t.Errorf("Execute() = %d, want 1", code)
	}
	if !strings.Contains(strings.ToLower(string(stderr)), "extension not connected") {
		t.Errorf("stderr = %q, want extension not connected", stderr)
	}
}

func TestCommandsSuggestStartingDaemonWhenTokenMissing(t *testing.T) {
	t.Setenv("CHROME_CTL_HOME", t.TempDir())
	_, err := executePhase2(t, "tabs")
	if err == nil {
		t.Fatal("tabs without token error = nil, want error")
	}
	if !strings.Contains(err.Error(), "chrome-ctl daemon --start") {
		t.Errorf("error = %q, want daemon --start suggestion", err)
	}
}
