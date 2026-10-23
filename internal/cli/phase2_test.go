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
	if params.URL != "https://example.test" || params.NewTab {
		t.Errorf("params = %+v, want trimmed URL and NewTab false", params)
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
	if params.TabID != nil || !params.All || !params.FormsOnly {
		t.Errorf("params = %+v, want TabID nil, All and FormsOnly true", params)
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
