package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

// A request that reached the extension but never got an ack must be reported
// as dispatched-but-unconfirmed, not as a connection failure.
func TestForwardTimeoutIsUnconfirmed(t *testing.T) {
	cfg := testConfig(t)
	cfg.ForwardTimeout = 150 * time.Millisecond
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	resultCh := startRPC(t, srv, token, `{"jsonrpc":"2.0","id":"cli-unconfirmed","method":"tab.list"}`)

	_ = ext.SetReadDeadline(time.Now().Add(2 * time.Second))
	var fwd protocol.Request
	if err := ext.ReadJSON(&fwd); err != nil {
		t.Fatalf("read forwarded request: %v", err)
	}

	var r rpcResult
	select {
	case r = <-resultCh:
	case <-time.After(3 * time.Second):
		t.Fatal("rpc request did not return")
	}
	if r.err != nil {
		t.Fatalf("rpc request: %v", r.err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(r.body, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrDispatchedUnconfirmed {
		t.Fatalf("error = %+v, want code %d", resp.Error, protocol.ErrDispatchedUnconfirmed)
	}
	if resp.Error.Data == nil {
		t.Fatal("timeout error lost its data payload")
	}
}
