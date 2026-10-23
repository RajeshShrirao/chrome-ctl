package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

type capturedRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func addrOf(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

func fakeRPCServer(t *testing.T, status int, body string, requests chan<- capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" {
			t.Errorf("path = %q, want /rpc", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-token")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var captured capturedRequest
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if requests != nil {
			requests <- captured
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func TestCallSuccess(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	srv := fakeRPCServer(t, http.StatusOK, `{"jsonrpc":"2.0","id":"x","result":{"status":"ok"}}`, requests)
	defer srv.Close()

	raw, rpcErr, err := New(addrOf(srv), "test-token").Call(
		context.Background(), "session.status", map[string]any{"tabId": 7})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if rpcErr != nil {
		t.Fatalf("Call() rpcErr = %+v, want nil", rpcErr)
	}
	if string(raw) != `{"status":"ok"}` {
		t.Errorf("result = %s, want {\"status\":\"ok\"}", raw)
	}

	req := <-requests
	if req.JSONRPC != protocol.JSONRPC {
		t.Errorf("jsonrpc = %q, want %q", req.JSONRPC, protocol.JSONRPC)
	}
	if !strings.HasPrefix(req.ID, "req_") {
		t.Errorf("id = %q, want req_ prefix", req.ID)
	}
	if req.Method != "session.status" {
		t.Errorf("method = %q, want session.status", req.Method)
	}
	if string(req.Params) != `{"tabId":7}` {
		t.Errorf("params = %s, want {\"tabId\":7}", req.Params)
	}
}

func TestCallRPCError(t *testing.T) {
	srv := fakeRPCServer(t, http.StatusOK,
		`{"jsonrpc":"2.0","id":"x","error":{"code":-32001,"message":"element not found"}}`, nil)
	defer srv.Close()

	raw, rpcErr, err := New(addrOf(srv), "test-token").Call(context.Background(), "action.click", nil)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if raw != nil {
		t.Errorf("result = %s, want nil", raw)
	}
	if rpcErr == nil {
		t.Fatal("rpcErr = nil, want RPC error")
	}
	if rpcErr.Code != -32001 {
		t.Errorf("code = %d, want -32001", rpcErr.Code)
	}
	if rpcErr.Message != "element not found" {
		t.Errorf("message = %q, want element not found", rpcErr.Message)
	}
}

func TestCallUnauthorized(t *testing.T) {
	srv := fakeRPCServer(t, http.StatusUnauthorized, ``, nil)
	defer srv.Close()

	_, _, err := New(addrOf(srv), "test-token").Call(context.Background(), "session.status", nil)
	if err == nil {
		t.Fatal("Call() error = nil, want unauthorized error")
	}
	if !strings.Contains(err.Error(), "~/.chrome-ctl/auth_token") {
		t.Errorf("error = %q, want mention of ~/.chrome-ctl/auth_token", err)
	}
}

func TestCallMalformedResponse(t *testing.T) {
	srv := fakeRPCServer(t, http.StatusOK, `not json`, nil)
	defer srv.Close()

	if _, _, err := New(addrOf(srv), "test-token").Call(context.Background(), "session.status", nil); err == nil {
		t.Fatal("Call() error = nil, want decode error")
	}
}

func TestCallHTTPError(t *testing.T) {
	srv := fakeRPCServer(t, http.StatusInternalServerError, `boom`, nil)
	defer srv.Close()

	_, _, err := New(addrOf(srv), "test-token").Call(context.Background(), "session.status", nil)
	if err == nil {
		t.Fatal("Call() error = nil, want HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %q, want mention of HTTP 500", err)
	}
}

func TestCallTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := addrOf(srv)
	srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := New(addr, "test-token").Call(ctx, "session.status", nil); err == nil {
		t.Fatal("Call() error = nil, want transport error")
	}
}

func TestStatus(t *testing.T) {
	requests := make(chan capturedRequest, 1)
	srv := fakeRPCServer(t, http.StatusOK, `{"jsonrpc":"2.0","id":"x","result":`+
		`{"status":"ok","extensionConnected":true,"tabCount":3,"debuggerAttached":false}}`, requests)
	defer srv.Close()

	status, err := New(addrOf(srv), "test-token").Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.ExtensionConnected {
		t.Error("ExtensionConnected = false, want true")
	}
	if status.TabCount != 3 {
		t.Errorf("TabCount = %d, want 3", status.TabCount)
	}

	req := <-requests
	if req.Method != protocol.MethodSessionStatus {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodSessionStatus)
	}
	if len(req.Params) != 0 {
		t.Errorf("params = %s, want omitted", req.Params)
	}
}

func TestStatusRPCError(t *testing.T) {
	srv := fakeRPCServer(t, http.StatusOK,
		`{"jsonrpc":"2.0","id":"x","error":{"code":-32000,"message":"extension disconnected"}}`, nil)
	defer srv.Close()

	_, err := New(addrOf(srv), "test-token").Status(context.Background())
	if err == nil {
		t.Fatal("Status() error = nil, want translated RPC error")
	}
	if !strings.Contains(err.Error(), "extension disconnected") {
		t.Errorf("error = %q, want RPC message", err)
	}
}
