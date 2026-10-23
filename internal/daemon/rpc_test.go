package daemon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestSessionStatusWithoutExtension(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)

	status, body := postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"cli-1","method":"session.status"}`)
	if status != 200 {
		t.Fatalf("status = %d, want 200", status)
	}
	var resp protocol.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.ID == nil || *resp.ID != "cli-1" {
		t.Fatalf("id = %v, want cli-1", resp.ID)
	}
	var session protocol.SessionStatus
	if err := json.Unmarshal(resp.Result, &session); err != nil {
		t.Fatalf("decode session status: %v", err)
	}
	if session.Status != "ok" {
		t.Fatalf("status = %q, want ok", session.Status)
	}
	if session.ExtensionConnected {
		t.Fatal("extensionConnected = true, want false")
	}
	if session.TabCount != 0 {
		t.Fatalf("tabCount = %d, want 0", session.TabCount)
	}
	if session.ActiveTab != nil {
		t.Fatalf("activeTab = %+v, want nil", session.ActiveTab)
	}
	if session.Version != cfg.Version {
		t.Fatalf("version = %q, want %q", session.Version, cfg.Version)
	}
}

func TestRPCMalformedAndInvalidRequests(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)

	if status, _ := postRPC(t, srv, token, `{`); status != 400 {
		t.Fatalf("malformed body status = %d, want 400", status)
	}

	status, body := postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"bad"}`)
	if status != 200 {
		t.Fatalf("invalid request status = %d, want 200", status)
	}
	var resp protocol.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want code %d", resp.Error, protocol.ErrInvalidRequest)
	}
}

func TestSessionStatusForwarding(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	resultCh := startRPC(t, srv, token, `{"jsonrpc":"2.0","id":"cli-1","method":"session.status"}`)

	_ = ext.SetReadDeadline(time.Now().Add(2 * time.Second))
	var fwd protocol.Request
	if err := ext.ReadJSON(&fwd); err != nil {
		t.Fatalf("read forwarded request: %v", err)
	}
	if fwd.Method != protocol.MethodSessionStatus {
		t.Fatalf("method = %q, want %q", fwd.Method, protocol.MethodSessionStatus)
	}
	if fwd.ID == nil || !strings.HasPrefix(*fwd.ID, "d-") {
		t.Fatalf("forwarded id = %v, want daemon id", fwd.ID)
	}
	if err := ext.WriteJSON(protocol.Response{
		JSONRPC: protocol.JSONRPC,
		ID:      fwd.ID,
		Result:  json.RawMessage(`{"activeTab":{"tabId":7,"title":"Tab","url":"https://example.com"},"tabCount":3,"debuggerAttached":true}`),
	}); err != nil {
		t.Fatalf("write extension response: %v", err)
	}

	r := <-resultCh
	if r.err != nil {
		t.Fatalf("rpc request: %v", r.err)
	}
	if r.status != 200 {
		t.Fatalf("status = %d, want 200", r.status)
	}
	var resp protocol.Response
	if err := json.Unmarshal(r.body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.ID == nil || *resp.ID != "cli-1" {
		t.Fatalf("id = %v, want original cli id", resp.ID)
	}
	var session protocol.SessionStatus
	if err := json.Unmarshal(resp.Result, &session); err != nil {
		t.Fatalf("decode session status: %v", err)
	}
	if !session.ExtensionConnected {
		t.Fatal("extensionConnected = false, want true")
	}
	if session.ActiveTab == nil || session.ActiveTab.TabID != 7 {
		t.Fatalf("activeTab = %+v, want tabId 7", session.ActiveTab)
	}
	if session.TabCount != 3 {
		t.Fatalf("tabCount = %d, want 3", session.TabCount)
	}
	if !session.DebuggerAttached {
		t.Fatal("debuggerAttached = false, want true")
	}
	if session.Version != cfg.Version {
		t.Fatalf("version = %q, want %q", session.Version, cfg.Version)
	}

	// Generic methods are relayed verbatim.
	resultCh = startRPC(t, srv, token, `{"jsonrpc":"2.0","id":"cli-2","method":"tab.list"}`)
	_ = ext.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := ext.ReadJSON(&fwd); err != nil {
		t.Fatalf("read forwarded generic request: %v", err)
	}
	if fwd.Method != "tab.list" {
		t.Fatalf("method = %q, want tab.list", fwd.Method)
	}
	if err := ext.WriteJSON(protocol.Response{
		JSONRPC: protocol.JSONRPC,
		ID:      fwd.ID,
		Result:  json.RawMessage(`{"tabs":[{"tabId":7}]}`),
	}); err != nil {
		t.Fatalf("write generic response: %v", err)
	}

	r = <-resultCh
	if r.err != nil {
		t.Fatalf("generic rpc request: %v", r.err)
	}
	var generic protocol.Response
	if err := json.Unmarshal(r.body, &generic); err != nil {
		t.Fatalf("decode generic response: %v", err)
	}
	if generic.ID == nil || *generic.ID != "cli-2" {
		t.Fatalf("generic id = %v, want cli-2", generic.ID)
	}
	if string(generic.Result) != `{"tabs":[{"tabId":7}]}` {
		t.Fatalf("generic result = %s, want relayed result", generic.Result)
	}
}

func TestPendingForwardFailsWhenExtensionCloses(t *testing.T) {
	cfg := testConfig(t)
	cfg.ForwardTimeout = 200 * time.Millisecond
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	start := time.Now()
	resultCh := startRPC(t, srv, token, `{"jsonrpc":"2.0","id":"cli-3","method":"tab.list"}`)

	_ = ext.SetReadDeadline(time.Now().Add(2 * time.Second))
	var fwd protocol.Request
	if err := ext.ReadJSON(&fwd); err != nil {
		t.Fatalf("read forwarded request: %v", err)
	}
	if err := ext.Close(); err != nil {
		t.Fatalf("close extension: %v", err)
	}

	var r rpcResult
	select {
	case r = <-resultCh:
	case <-time.After(3 * time.Second):
		t.Fatal("pending forward did not fail after extension close")
	}
	if r.err != nil {
		t.Fatalf("rpc request: %v", r.err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(r.body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrConnection {
		t.Fatalf("error = %+v, want code %d", resp.Error, protocol.ErrConnection)
	}
	if resp.ID == nil || *resp.ID != "cli-3" {
		t.Fatalf("id = %v, want cli-3", resp.ID)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("pending forward took %v, want prompt failure", elapsed)
	}
}

func TestForwardingErrorIsRelayed(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	resultCh := startRPC(t, srv, token, `{"jsonrpc":"2.0","id":"cli-4","method":"action.click"}`)
	_ = ext.SetReadDeadline(time.Now().Add(2 * time.Second))
	var fwd protocol.Request
	// The forwarded frame must be consumed before the reply is written.
	if err := ext.ReadJSON(&fwd); err != nil {
		t.Fatalf("read forwarded request: %v", err)
	}
	extErr := &protocol.RPCError{
		Code:    -32001,
		Message: "element not found",
		Data:    map[string]any{"elementId": 5},
	}
	if err := ext.WriteJSON(protocol.Response{JSONRPC: protocol.JSONRPC, ID: fwd.ID, Error: extErr}); err != nil {
		t.Fatalf("write extension error: %v", err)
	}

	r := <-resultCh
	if r.err != nil {
		t.Fatalf("rpc request: %v", r.err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(r.body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != extErr.Code || resp.Error.Message != extErr.Message {
		t.Fatalf("error = %+v, want relayed extension error", resp.Error)
	}
	if resp.Error.Data == nil {
		t.Fatal("error data was dropped during relay")
	}
}
