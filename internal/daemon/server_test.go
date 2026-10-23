package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	paths := config.Paths{
		Home:      dir,
		TokenFile: filepath.Join(dir, "auth_token"),
		PidFile:   filepath.Join(dir, "daemon.pid"),
		LogFile:   filepath.Join(dir, "daemon.log"),
	}
	if _, _, err := config.EnsureToken(paths.TokenFile); err != nil {
		t.Fatalf("ensure token: %v", err)
	}
	return Config{
		Addr:           "127.0.0.1:0",
		Version:        "test-1.0",
		Paths:          paths,
		Log:            log.New(io.Discard, "", 0),
		ReadTimeout:    2 * time.Second,
		ForwardTimeout: time.Second,
	}
}

func testToken(t *testing.T, cfg Config) string {
	t.Helper()
	token, err := config.ReadToken(cfg.Paths.TokenFile)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	return token
}

func startDaemon(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	d := newDaemon(cfg)
	srv := httptest.NewServer(d.handler())
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(endpoint, token string) string {
	return "ws" + strings.TrimPrefix(endpoint, "http") + "/ws?token=" + url.QueryEscape(token)
}

func dialWS(t *testing.T, target string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(target, nil)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatalf("dial %s: %v", target, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readReady(t *testing.T, conn *websocket.Conn) protocol.SystemReadyParams {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var req protocol.Request
	if err := conn.ReadJSON(&req); err != nil {
		t.Fatalf("read system.ready: %v", err)
	}
	if req.Method != protocol.MethodSystemReady {
		t.Fatalf("method = %q, want %q", req.Method, protocol.MethodSystemReady)
	}
	var params protocol.SystemReadyParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode system.ready: %v", err)
	}
	return params
}

type rpcResult struct {
	status int
	body   []byte
	err    error
}

func startRPC(t *testing.T, srv *httptest.Server, token, body string) <-chan rpcResult {
	t.Helper()
	ch := make(chan rpcResult, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/rpc", strings.NewReader(body))
		if err != nil {
			ch <- rpcResult{err: err}
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			ch <- rpcResult{err: err}
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		ch <- rpcResult{status: resp.StatusCode, body: data, err: err}
	}()
	return ch
}

func postRPC(t *testing.T, srv *httptest.Server, token, body string) (int, []byte) {
	t.Helper()
	select {
	case r := <-startRPC(t, srv, token, body):
		if r.err != nil {
			t.Fatalf("rpc request: %v", r.err)
		}
		return r.status, r.body
	case <-time.After(5 * time.Second):
		t.Fatal("rpc request timed out")
		return 0, nil
	}
}

func TestAuthRejectsInvalidTokens(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	valid := testToken(t, cfg)
	wrong := strings.Repeat("b", 64)
	if wrong == valid {
		wrong = strings.Repeat("c", 64)
	}

	cases := []struct {
		name   string
		method string
		path   string
		auth   string
		query  string
	}{
		{name: "health missing", method: http.MethodGet, path: "/health"},
		{name: "health malformed", method: http.MethodGet, path: "/health", auth: "Basic abc"},
		{name: "health wrong bearer", method: http.MethodGet, path: "/health", auth: "Bearer " + wrong},
		{name: "health wrong query", method: http.MethodGet, path: "/health", query: wrong},
		{name: "rpc missing", method: http.MethodPost, path: "/rpc"},
		{name: "rpc malformed", method: http.MethodPost, path: "/rpc", auth: "Bearer not-a-token"},
		{name: "rpc wrong query", method: http.MethodPost, path: "/rpc", query: wrong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := srv.URL + tc.path
			if tc.query != "" {
				target += "?token=" + url.QueryEscape(tc.query)
			}
			var body io.Reader
			if tc.method == http.MethodPost {
				body = strings.NewReader(`{"jsonrpc":"2.0","id":"1","method":"session.status"}`)
			}
			req, err := http.NewRequest(tc.method, target, body)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			if got := resp.Header.Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
			}
		})
	}

	t.Run("ws", func(t *testing.T) {
		for _, target := range []string{
			"ws" + strings.TrimPrefix(srv.URL, "http") + "/ws",
			wsURL(srv.URL, wrong),
		} {
			_, resp, err := websocket.DefaultDialer.Dial(target, nil)
			if resp != nil {
				resp.Body.Close()
			}
			if !errors.Is(err, websocket.ErrBadHandshake) {
				t.Fatalf("dial error = %v, want ErrBadHandshake", err)
			}
			if resp != nil && resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("handshake status = %d, want 401", resp.StatusCode)
			}
		}
	})
}

func TestHealthAcceptsTokens(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)

	tests := []struct {
		name string
		url  string
		auth string
	}{
		{name: "query", url: srv.URL + "/health?token=" + url.QueryEscape(token)},
		{name: "bearer", url: srv.URL + "/health", auth: "Bearer " + token},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			var health struct {
				Status    string `json:"status"`
				Version   string `json:"version"`
				UptimeSec int64  `json:"uptimeSec"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
				t.Fatalf("decode health: %v", err)
			}
			if health.Status != "ok" {
				t.Fatalf("status = %q, want ok", health.Status)
			}
			if health.Version != cfg.Version {
				t.Fatalf("version = %q, want %q", health.Version, cfg.Version)
			}
			if health.UptimeSec < 0 {
				t.Fatalf("uptimeSec = %d, want non-negative", health.UptimeSec)
			}
		})
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/health", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("wrong method request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestWebSocketReadyAndPing(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	conn := dialWS(t, wsURL(srv.URL, testToken(t, cfg)))

	ready := readReady(t, conn)
	if ready.Version != cfg.Version {
		t.Fatalf("version = %q, want %q", ready.Version, cfg.Version)
	}
	if ready.AttachedTabs == nil {
		t.Fatal("attachedTabs = null, want empty array")
	}
	if len(ready.AttachedTabs) != 0 {
		t.Fatalf("attachedTabs = %v, want empty", ready.AttachedTabs)
	}

	params := json.RawMessage(`{"timestamp":1729725600}`)
	if err := conn.WriteJSON(protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodSystemPing,
		Params:  params,
	}); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var pong protocol.Response
	if err := conn.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.ID != nil {
		t.Fatalf("pong id = %v, want null", *pong.ID)
	}
	var result protocol.PongResult
	if err := json.Unmarshal(pong.Result, &result); err != nil {
		t.Fatalf("decode pong: %v", err)
	}
	if result.Status != "pong" || result.Timestamp != 1729725600 {
		t.Fatalf("pong = %+v, want status pong and original timestamp", result)
	}

	id := "ping-1"
	if err := conn.WriteJSON(protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id,
		Method:  protocol.MethodSystemPing,
		Params:  params,
	}); err != nil {
		t.Fatalf("write ping with id: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong with id: %v", err)
	}
	if pong.ID == nil || *pong.ID != id {
		t.Fatalf("pong id = %v, want %q", pong.ID, id)
	}
}

func TestNewExtensionReplacesOld(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)

	first := dialWS(t, wsURL(srv.URL, token))
	readReady(t, first)
	second := dialWS(t, wsURL(srv.URL, token))
	readReady(t, second)

	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := first.ReadMessage()
	if err == nil {
		t.Fatal("first connection still open after replacement")
	}
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("close error = %v, want *websocket.CloseError", err)
	}
	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("close code = %d, want %d", closeErr.Code, websocket.ClosePolicyViolation)
	}
	if closeErr.Text != "replaced" {
		t.Fatalf("close text = %q, want replaced", closeErr.Text)
	}

	if err := second.WriteJSON(protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodSystemPing,
		Params:  json.RawMessage(`{"timestamp":1}`),
	}); err != nil {
		t.Fatalf("write ping on new connection: %v", err)
	}
	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	var pong protocol.Response
	if err := second.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong on new connection: %v", err)
	}
}

func TestServeShutsDownOnContextCancel(t *testing.T) {
	cfg := testConfig(t)
	token := testToken(t, cfg)
	d := newDaemon(cfg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.serve(ctx, ln) }()

	base := "http://" + ln.Addr().String()
	waitForHealth(t, base, token)

	ext := dialWS(t, wsURL(base, token))
	readReady(t, ext)

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return after context cancellation")
	}

	_ = ext.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := ext.ReadMessage(); err == nil {
		t.Fatal("extension socket still open after shutdown")
	}
}

func waitForHealth(t *testing.T, base, token string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/health?token=" + url.QueryEscape(token))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon did not become healthy")
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAuthRejectionDoesNotLogToken(t *testing.T) {
	cfg := testConfig(t)
	var logs syncBuffer
	cfg.Log = log.New(&logs, "", 0)
	srv := startDaemon(t, cfg)

	secret := strings.Repeat("e", 64)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(srv.URL + "/health?token=" + secret)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("log output contains the presented token: %q", logs.String())
	}
}
