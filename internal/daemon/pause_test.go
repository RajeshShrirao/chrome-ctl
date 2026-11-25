package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestPauseBlocksForwardUntilResumed(t *testing.T) {
	d := newDaemon(Config{Version: "test"})

	id1 := "1"
	resp, ok := d.handleLocalRPC(context.Background(), protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id1,
		Method:  protocol.MethodSessionPause,
	})
	if !ok || resp.Error != nil {
		t.Fatalf("session.pause failed: ok=%v resp=%+v", ok, resp)
	}
	var pauseRes protocol.SessionPauseResult
	if err := json.Unmarshal(resp.Result, &pauseRes); err != nil || !pauseRes.Paused {
		t.Fatalf("want paused=true, got err=%v res=%+v", err, pauseRes)
	}
	if !d.isPaused() {
		t.Fatalf("daemon should report paused")
	}

	// session.status must still respond immediately while paused.
	idStatus := "status"
	statusResp := d.handleSessionStatus(context.Background(), protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &idStatus,
		Method:  protocol.MethodSessionStatus,
	})
	if statusResp.Error != nil {
		t.Fatalf("session.status errored while paused: %+v", statusResp.Error)
	}
	var status protocol.SessionStatus
	if err := json.Unmarshal(statusResp.Result, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Paused {
		t.Fatalf("session.status should report Paused=true")
	}

	// A forwarded RPC must block while paused, then proceed once resumed.
	done := make(chan *protocol.RPCError, 1)
	go func() {
		_, rpcErr := d.forward(context.Background(), protocol.Request{
			JSONRPC: protocol.JSONRPC,
			Method:  protocol.MethodTabList,
		})
		done <- rpcErr
	}()

	select {
	case err := <-done:
		t.Fatalf("forward returned before resume: %+v", err)
	case <-time.After(60 * time.Millisecond):
	}

	id2 := "2"
	resResp, ok := d.handleLocalRPC(context.Background(), protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id2,
		Method:  protocol.MethodSessionResume,
	})
	if !ok || resResp.Error != nil {
		t.Fatalf("session.resume failed: ok=%v resp=%+v", ok, resResp)
	}

	select {
	case rpcErr := <-done:
		// No extension is connected in this unit test, so ErrConnection proves
		// forward passed the pause gate and reached forwardRaw.
		if rpcErr == nil || rpcErr.Code != protocol.ErrConnection {
			t.Fatalf("expected ErrConnection after resume, got %+v", rpcErr)
		}
	case <-time.After(time.Second):
		t.Fatalf("forward remained blocked after resume")
	}
}

func TestPauseRespectsContextCancellation(t *testing.T) {
	d := newDaemon(Config{Version: "test"})
	d.setPaused(true)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	_, rpcErr := d.forward(ctx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodPageSnap,
	})
	if rpcErr == nil || rpcErr.Code != protocol.ErrBusy {
		t.Fatalf("expected ErrBusy on cancelled paused wait, got %+v", rpcErr)
	}
}
