package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const maxRPCBodyBytes = 1 << 20

const (
	actionTypeTimeout   = 300 * time.Second
	actionMediumTimeout = 60 * time.Second
	actionPressTimeout  = 20 * time.Second
	pageWaitTimeout     = 30 * time.Second
	mediaCaptureTimeout = 30 * time.Second
	pageDescribeTimeout = 5 * time.Second
	pageResolveTimeout  = 20 * time.Second
	authSSOTimeout      = 60 * time.Second
	windowCreateTimeout = 30 * time.Second
)

// forwardTimeoutFor gives long-running actions a budget beyond the default
// forward timeout; the extension, offscreen relay, and CLI use matching budgets.
func forwardTimeoutFor(method string, fallback time.Duration) time.Duration {
	switch method {
	case protocol.MethodActionType:
		return actionTypeTimeout
	case protocol.MethodActionClick, protocol.MethodActionScroll, protocol.MethodActionUpload:
		return actionMediumTimeout
	case protocol.MethodActionPress:
		return actionPressTimeout
	case protocol.MethodPageWait:
		return pageWaitTimeout
	case protocol.MethodMediaCapture:
		return mediaCaptureTimeout
	case protocol.MethodPageDescribe:
		return pageDescribeTimeout
	case protocol.MethodPageResolve:
		return pageResolveTimeout
	case protocol.MethodAuthSSO:
		return authSSOTimeout
	case protocol.MethodWindowCreate, protocol.MethodWindowEnsure:
		return windowCreateTimeout
	default:
		return fallback
	}
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

	timer := time.NewTimer(forwardTimeoutFor(req.Method, d.cfg.ForwardTimeout))
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-timer.C:
		d.removePending(id)
		return nil, &protocol.RPCError{Code: protocol.ErrConnection, Message: "extension response timeout"}
	case <-ctx.Done():
		d.removePending(id)
		return nil, &protocol.RPCError{Code: protocol.ErrConnection, Message: "request cancelled"}
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
