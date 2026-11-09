package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/macro"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const maxRPCBodyBytes = 1 << 20

const (
	actionTypeTimeout   = protocol.ActionTypeTimeout
	actionMediumTimeout = protocol.ActionMediumTimeout
	actionPressTimeout  = protocol.ActionPressTimeout
	pageWaitTimeout     = protocol.PageWaitTimeout
	mediaCaptureTimeout = protocol.MediaCaptureTimeout
	pageDescribeTimeout = protocol.PageDescribeTimeout
	pageResolveTimeout  = protocol.PageResolveTimeout
	pageEvalTimeout     = protocol.PageEvalTimeout
	authSSOTimeout      = protocol.AuthSSOTimeout
	windowCreateTimeout = protocol.WindowCreateTimeout
)

// forwardTimeoutFor gives long-running actions a budget beyond the default
// forward timeout; the extension, offscreen relay, and CLI use matching budgets
// generated from internal/protocol/contract.json.
func forwardTimeoutFor(method string, fallback time.Duration) time.Duration {
	return protocol.TimeoutFor(method, fallback)
}

func (d *daemon) handleRPC(w http.ResponseWriter, r *http.Request) {
	var req protocol.Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRPCBodyBytes))
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON request body", http.StatusBadRequest)
		return
	}
	if req.Method == "" {
		writeJSON(w, http.StatusOK, protocol.Failure(req.ID, protocol.ErrInvalidRequest, "invalid JSON-RPC method"))
		return
	}

	if req.Method == protocol.MethodPageEval && d.cfg.DisableEval {
		writeJSON(w, http.StatusOK, protocol.Failure(
			req.ID,
			protocol.ErrFeatureDisabled,
			"eval is disabled on this daemon; stop it and run `chrome-ctl daemon --start` without --no-eval to re-enable",
		))
		return
	}

	var resp protocol.Response
	if local, ok := d.handleLocalRPC(r.Context(), req); ok {
		resp = local
	} else if req.Method == protocol.MethodSessionStatus {
		resp = d.handleSessionStatus(r.Context(), req)
	} else {
		pending := d.macroBeforeForward(r.Context(), req)
		resp = d.forwardResponse(r.Context(), req)
		d.macroAfterForward(req, resp, pending)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (d *daemon) handleSessionStatus(ctx context.Context, req protocol.Request) protocol.Response {
	if d.extension() == nil {
		return protocol.Success(req.ID, protocol.SessionStatus{
			Status:             "ok",
			ExtensionConnected: false,
			Version:            d.cfg.Version,
			ProtocolVersion:    protocol.ProtocolVersion,
			Capabilities:       protocol.Capabilities,
			UptimeSec:          d.uptimeSec(),
		})
	}

	result, rpcErr := d.forward(ctx, req)
	if rpcErr != nil {
		d.logForwardFailure(req.Method, rpcErr)
		return responseWithError(req.ID, rpcErr)
	}

	var ext protocol.ExtensionStatus
	if err := json.Unmarshal(result, &ext); err != nil {
		d.log.Printf("daemon: decode %s result: %v", req.Method, err)
		return protocol.Failure(req.ID, protocol.ErrInternal, "invalid extension response")
	}
	return protocol.Success(req.ID, protocol.SessionStatus{
		Status:             "ok",
		ExtensionConnected: true,
		Version:            d.cfg.Version,
		ProtocolVersion:    protocol.ProtocolVersion,
		Capabilities:       protocol.Capabilities,
		ExtensionVersion:   ext.ProtocolVersion,
		ExtensionCaps:      ext.Capabilities,
		UptimeSec:          d.uptimeSec(),
		ActiveTab:          ext.ActiveTab,
		TabCount:           ext.TabCount,
		DebuggerAttached:   ext.DebuggerAttached,
	})
}

func (d *daemon) forwardResponse(ctx context.Context, req protocol.Request) protocol.Response {
	result, rpcErr := d.forward(ctx, req)
	if rpcErr != nil {
		d.logForwardFailure(req.Method, rpcErr)
		return responseWithError(req.ID, rpcErr)
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	return protocol.Response{JSONRPC: protocol.JSONRPC, ID: req.ID, Result: result}
}

func responseWithError(id *string, rpcErr *protocol.RPCError) protocol.Response {
	return protocol.Response{JSONRPC: protocol.JSONRPC, ID: id, Error: rpcErr}
}

func (d *daemon) logForwardFailure(method string, rpcErr *protocol.RPCError) {
	d.log.Printf("daemon: forward %s failed: %s (code %d)", method, rpcErr.Message, rpcErr.Code)
}

func (d *daemon) forward(ctx context.Context, req protocol.Request) (json.RawMessage, *protocol.RPCError) {
	plan := scheduleFor(req.Method, req.Params)
	release, rpcErr := d.sched.acquire(ctx, plan.key, plan.write)
	if rpcErr != nil {
		return nil, rpcErr
	}
	defer release()

	if plan.singleFlight {
		return d.sched.singleFlight(
			ctx,
			singleFlightKey(plan, req),
			func() (json.RawMessage, *protocol.RPCError) {
				return d.forwardRaw(ctx, req)
			},
		)
	}

	if key := macro.IdempotencyKey(ctx); plan.write && key != "" {
		result, rpcErr := d.sched.idempotent(plan.key+"\x00"+req.Method+"\x00"+key, func() (json.RawMessage, *protocol.RPCError) {
			return d.forwardRaw(ctx, req)
		})
		d.sched.clearCache()
		return result, rpcErr
	}

	result, rpcErr := d.forwardRaw(ctx, req)
	if plan.write {
		// A cached read from before this mutation must not be served after it.
		d.sched.clearCache()
	}
	return result, rpcErr
}

// forwardRaw writes one request to the extension and waits for its response.
// Callers must already hold the per-tab gate.
func (d *daemon) forwardRaw(ctx context.Context, req protocol.Request) (json.RawMessage, *protocol.RPCError) {
	ext := d.extension()
	if ext == nil {
		return nil, &protocol.RPCError{Code: protocol.ErrConnection, Message: "chrome extension not connected"}
	}

	id, ch, ok := d.addPending(ext)
	if !ok {
		return nil, &protocol.RPCError{Code: protocol.ErrConnection, Message: "chrome extension disconnected"}
	}
	if err := ext.writeJSON(protocol.Request{
		JSONRPC: protocol.JSONRPC,
		ID:      &id,
		Method:  req.Method,
		Params:  req.Params,
	}); err != nil {
		d.removePending(id)
		return nil, &protocol.RPCError{Code: protocol.ErrConnection, Message: "failed to send request to extension"}
	}

	timeout := forwardTimeoutFor(req.Method, d.cfg.ForwardTimeout)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-timer.C:
		d.removePending(id)
		return nil, &protocol.RPCError{
			Code:    protocol.ErrDispatchedUnconfirmed,
			Message: fmt.Sprintf("request dispatched but no ack after %s", timeout),
			Data: map[string]any{
				"method":    req.Method,
				"timeoutMs": timeout.Milliseconds(),
			},
		}
	case <-ctx.Done():
		d.removePending(id)
		return nil, &protocol.RPCError{
			Code:    protocol.ErrDispatchedUnconfirmed,
			Message: "request cancelled after dispatch",
			Data:    map[string]any{"method": req.Method},
		}
	}
}

func (d *daemon) addPending(ext *extensionConn) (string, chan protocol.Response, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != ext {
		return "", nil, false
	}
	id := fmt.Sprintf("d-%d", d.nextID.Add(1))
	ch := make(chan protocol.Response, 1)
	d.pending[id] = ch
	return id, ch, true
}

func (d *daemon) removePending(id string) {
	d.mu.Lock()
	delete(d.pending, id)
	d.mu.Unlock()
}
