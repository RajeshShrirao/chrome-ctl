package daemon

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rajeshshrirao/chrome-ctl/internal/macro"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

type macroFakeHandler func(req protocol.Request) (any, *protocol.RPCError)

// macroServeFakeExtension answers forwarded requests until the socket closes.
// Handlers return the raw result value or an RPC error.
func macroServeFakeExtension(t *testing.T, conn *websocket.Conn, handle macroFakeHandler) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Time{})
	go func() {
		for {
			var req protocol.Request
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			result, rpcErr := handle(req)
			resp := protocol.Response{JSONRPC: protocol.JSONRPC, ID: req.ID}
			if rpcErr != nil {
				resp.Error = rpcErr
			} else {
				raw, err := json.Marshal(result)
				if err != nil {
					return
				}
				resp.Result = raw
			}
			if err := conn.WriteJSON(resp); err != nil {
				return
			}
		}
	}()
}

func TestMacroRecordSaveAndList(t *testing.T) {
	cfg := testConfig(t)
	cfg.Paths.MacrosDir = filepath.Join(cfg.Paths.Home, "macros")
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	macroServeFakeExtension(t, ext, func(req protocol.Request) (any, *protocol.RPCError) {
		switch req.Method {
		case protocol.MethodPageDescribe:
			var params protocol.PageDescribeParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &protocol.RPCError{Code: protocol.ErrInvalidRequest, Message: "bad describe params"}
			}
			targets := map[int]protocol.MacroTarget{
				7: {AriaLabel: "Submit", Role: "button", Tag: "button", Text: "Submit"},
				8: {AriaLabel: "Message", Tag: "input", Type: "text", CSSSelector: "#message"},
			}
			return protocol.PageDescribeResult{Target: targets[params.ID]}, nil
		case protocol.MethodActionClick:
			return protocol.ActionClickResult{
				Success: true,
				Action:  "click",
				Target:  protocol.ActionTarget{ID: 7, Tag: "button", Label: "Submit"},
			}, nil
		case protocol.MethodActionType:
			return protocol.ActionTypeResult{Success: true, CharsTyped: 5}, nil
		case protocol.MethodTabOpen:
			return protocol.TabOpenResult{TabID: 3, URL: "https://example.com/start", Loaded: true}, nil
		case protocol.MethodPageSnap:
			return protocol.SnapResult{FormattedText: "TAB [3] Example"}, nil
		default:
			return nil, &protocol.RPCError{Code: protocol.ErrMethodNotFound, Message: "unexpected method " + req.Method}
		}
	})

	post := func(body string) protocol.Response {
		t.Helper()
		_, data := postRPC(t, srv, token, body)
		var resp protocol.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	resp := post(`{"jsonrpc":"2.0","id":"m1","method":"macro.start","params":{"name":"demo"}}`)
	if resp.Error != nil {
		t.Fatalf("macro.start error: %+v", resp.Error)
	}
	var started protocol.MacroStartResult
	if err := json.Unmarshal(resp.Result, &started); err != nil {
		t.Fatalf("decode macro.start: %v", err)
	}
	if !started.Started || started.Name != "demo" || started.Steps != 0 {
		t.Fatalf("macro.start = %+v, want started demo with 0 steps", started)
	}

	if resp := post(`{"jsonrpc":"2.0","id":"m2","method":"macro.start","params":{"name":"other"}}`); resp.Error == nil {
		t.Fatal("second macro.start succeeded, want already-active error")
	}

	if resp := post(`{"jsonrpc":"2.0","id":"a1","method":"tab.open","params":{"url":"https://example.com/start"}}`); resp.Error != nil {
		t.Fatalf("tab.open error: %+v", resp.Error)
	}
	if resp := post(`{"jsonrpc":"2.0","id":"a2","method":"action.click","params":{"id":7}}`); resp.Error != nil {
		t.Fatalf("action.click error: %+v", resp.Error)
	}
	if resp := post(`{"jsonrpc":"2.0","id":"a3","method":"action.type","params":{"id":8,"text":"hello world","enter":true}}`); resp.Error != nil {
		t.Fatalf("action.type error: %+v", resp.Error)
	}

	resp = post(`{"jsonrpc":"2.0","id":"s1","method":"macro.save","params":{"params":["dest","query"],"description":"Demo flow"}}`)
	if resp.Error != nil {
		t.Fatalf("macro.save error: %+v", resp.Error)
	}
	var saved protocol.MacroSaveResult
	if err := json.Unmarshal(resp.Result, &saved); err != nil {
		t.Fatalf("decode macro.save: %v", err)
	}
	if saved.Name != "demo" || saved.Steps != 3 {
		t.Fatalf("macro.save = %+v, want demo with 3 steps", saved)
	}
	if len(saved.Parameters) != 2 || saved.Parameters[0] != "dest" || saved.Parameters[1] != "query" {
		t.Fatalf("parameters = %v, want [dest query]", saved.Parameters)
	}

	loaded, err := macro.Load(cfg.Paths.MacrosDir, "demo")
	if err != nil {
		t.Fatalf("load saved macro: %v", err)
	}
	if len(loaded.Steps) != 3 {
		t.Fatalf("saved steps = %d, want 3", len(loaded.Steps))
	}
	if loaded.Description != "Demo flow" {
		t.Fatalf("description = %q, want Demo flow", loaded.Description)
	}
	if loaded.Steps[0].Action != "open" || loaded.Steps[0].URL != "{{dest}}" {
		t.Fatalf("step 1 = %+v, want open with {{dest}}", loaded.Steps[0])
	}
	if loaded.Steps[1].Action != "click" || loaded.Steps[1].Target == nil || loaded.Steps[1].Target.AriaLabel != "Submit" {
		t.Fatalf("step 2 = %+v, want click on Submit", loaded.Steps[1])
	}
	if loaded.Steps[1].Description != `Click "Submit"` {
		t.Fatalf("click description = %q", loaded.Steps[1].Description)
	}
	if loaded.Steps[2].Action != "type" || loaded.Steps[2].Value != "{{query}}" {
		t.Fatalf("step 3 = %+v, want type with {{query}}", loaded.Steps[2])
	}
	if loaded.Steps[2].Options == nil || !loaded.Steps[2].Options.Enter {
		t.Fatalf("type options = %+v, want enter", loaded.Steps[2].Options)
	}
	if loaded.Steps[2].Target == nil || loaded.Steps[2].Target.AriaLabel != "Message" {
		t.Fatalf("type target = %+v, want Message", loaded.Steps[2].Target)
	}
	if len(loaded.Parameters) != 2 || loaded.Parameters[0].Name != "dest" || !loaded.Parameters[0].Required {
		t.Fatalf("saved parameters = %+v", loaded.Parameters)
	}

	resp = post(`{"jsonrpc":"2.0","id":"l1","method":"macro.list"}`)
	if resp.Error != nil {
		t.Fatalf("macro.list error: %+v", resp.Error)
	}
	var list protocol.MacroListResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatalf("decode macro.list: %v", err)
	}
	if len(list.Macros) != 1 || list.Macros[0].Name != "demo" || list.Macros[0].Steps != 3 {
		t.Fatalf("macro.list = %+v, want demo with 3 steps", list.Macros)
	}
	if len(list.Macros[0].Parameters) != 2 || list.Macros[0].Parameters[0] != "dest" {
		t.Fatalf("list parameters = %v", list.Macros[0].Parameters)
	}
}

func TestMacroRunReplaysInterpolatedSteps(t *testing.T) {
	cfg := testConfig(t)
	cfg.Paths.MacrosDir = filepath.Join(cfg.Paths.Home, "macros")
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	file := protocol.MacroFile{
		Name: "replay",
		Steps: []protocol.MacroStep{
			{StepID: 1, Action: "open", URL: "https://{{trim host}}/login", Description: "Navigate"},
			{
				StepID:  2,
				Action:  "type",
				Target:  &protocol.MacroTarget{AriaLabel: "Email"},
				Value:   "{{lowercase email}}",
				Options: &protocol.MacroStepOptions{Enter: true},
			},
		},
	}
	if _, err := macro.Save(cfg.Paths.MacrosDir, file); err != nil {
		t.Fatalf("save macro: %v", err)
	}

	opens := make(chan protocol.TabOpenParams, 1)
	types := make(chan protocol.ActionTypeParams, 1)
	resolves := make(chan protocol.PageResolveParams, 1)
	macroServeFakeExtension(t, ext, func(req protocol.Request) (any, *protocol.RPCError) {
		switch req.Method {
		case protocol.MethodPageResolve:
			var params protocol.PageResolveParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &protocol.RPCError{Code: protocol.ErrInvalidRequest, Message: "bad resolve params"}
			}
			resolves <- params
			return protocol.PageResolveResult{
				ID: 42, Tag: "input", Type: "text", Label: "Email", MatchedTier: "aria",
			}, nil
		case protocol.MethodTabOpen:
			var params protocol.TabOpenParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &protocol.RPCError{Code: protocol.ErrInvalidRequest, Message: "bad open params"}
			}
			opens <- params
			return protocol.TabOpenResult{TabID: 3, URL: params.URL, Loaded: true}, nil
		case protocol.MethodActionType:
			var params protocol.ActionTypeParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return nil, &protocol.RPCError{Code: protocol.ErrInvalidRequest, Message: "bad type params"}
			}
			types <- params
			return protocol.ActionTypeResult{Success: true, CharsTyped: len(params.Text)}, nil
		default:
			return nil, &protocol.RPCError{Code: protocol.ErrMethodNotFound, Message: "unexpected method " + req.Method}
		}
	})

	_, data := postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"r1","method":"macro.run","params":{"name":"replay","vars":{"host":" Example.COM ","email":"USER@EXAMPLE.COM"}}}`)
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("macro.run error: %+v", resp.Error)
	}
	var run protocol.MacroRunResult
	if err := json.Unmarshal(resp.Result, &run); err != nil {
		t.Fatalf("decode macro.run: %v", err)
	}
	if run.Macro != "replay" || run.StepsRun != 2 || !run.Completed {
		t.Fatalf("macro.run = %+v, want replay 2 steps completed", run)
	}

	open := <-opens
	if open.URL != "https://Example.COM/login" {
		t.Fatalf("open url = %q, want trimmed interpolation", open.URL)
	}
	resolve := <-resolves
	if resolve.Target.AriaLabel != "Email" || resolve.TimeoutMs != 3000 {
		t.Fatalf("resolve params = %+v, want Email target and 3000ms", resolve)
	}
	typeParams := <-types
	if typeParams.ID != 42 {
		t.Fatalf("type id = %d, want resolved 42", typeParams.ID)
	}
	if typeParams.Text != "user@example.com" {
		t.Fatalf("type text = %q, want lowercase interpolation", typeParams.Text)
	}
	if !typeParams.Enter {
		t.Fatal("type enter = false, want true")
	}
}

func TestMacroRunResolutionFailureReturnsSnapshot(t *testing.T) {
	cfg := testConfig(t)
	cfg.Paths.MacrosDir = filepath.Join(cfg.Paths.Home, "macros")
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	file := protocol.MacroFile{
		Name: "failing",
		Steps: []protocol.MacroStep{
			{StepID: 1, Action: "open", URL: "https://example.com"},
			{StepID: 2, Action: "click", Target: &protocol.MacroTarget{AriaLabel: "Missing"}, OnFailure: "retry_3x"},
			{StepID: 3, Action: "click", Target: &protocol.MacroTarget{AriaLabel: "Extra"}},
		},
	}
	if _, err := macro.Save(cfg.Paths.MacrosDir, file); err != nil {
		t.Fatalf("save macro: %v", err)
	}

	const snapshotText = "TAB [1] Example\n[12] <button:submit> \"Go\" @10,20"
	resolveCalls := 0
	snaps := make(chan struct{}, 1)
	macroServeFakeExtension(t, ext, func(req protocol.Request) (any, *protocol.RPCError) {
		switch req.Method {
		case protocol.MethodTabOpen:
			return protocol.TabOpenResult{TabID: 1, URL: "https://example.com", Loaded: true}, nil
		case protocol.MethodPageResolve:
			resolveCalls++
			return nil, &protocol.RPCError{Code: protocol.ErrElementNotFound, Message: "element not found"}
		case protocol.MethodPageSnap:
			snaps <- struct{}{}
			return protocol.SnapResult{TabID: 1, Title: "Example", FormattedText: snapshotText}, nil
		default:
			return nil, &protocol.RPCError{Code: protocol.ErrMethodNotFound, Message: "unexpected method " + req.Method}
		}
	})

	_, data := postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"r2","method":"macro.run","params":{"name":"failing"}}`)
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMacroStepFailed {
		t.Fatalf("error = %+v, want code %d", resp.Error, protocol.ErrMacroStepFailed)
	}
	if resp.ID == nil || *resp.ID != "r2" {
		t.Fatalf("id = %v, want r2", resp.ID)
	}
	if !strings.Contains(resp.Error.Message, "macro step 2 failed") {
		t.Fatalf("message = %q, want macro step 2 failed", resp.Error.Message)
	}

	raw, err := json.Marshal(resp.Error.Data)
	if err != nil {
		t.Fatalf("encode error data: %v", err)
	}
	var failure protocol.MacroStepFailure
	if err := json.Unmarshal(raw, &failure); err != nil {
		t.Fatalf("decode failure data: %v", err)
	}
	if failure.Status != "macro_step_failed" {
		t.Fatalf("status = %q, want macro_step_failed", failure.Status)
	}
	if failure.Macro != "failing" || failure.FailedStepID != 2 {
		t.Fatalf("failure = %+v, want failing step 2", failure)
	}
	if failure.CurrentSnapshot != snapshotText {
		t.Fatalf("snapshot = %q, want %q", failure.CurrentSnapshot, snapshotText)
	}
	if failure.RemainingSteps != 1 {
		t.Fatalf("remaining = %d, want 1", failure.RemainingSteps)
	}
	if resolveCalls != 3 {
		t.Fatalf("resolve calls = %d, want 3 retries", resolveCalls)
	}
	select {
	case <-snaps:
	default:
		t.Fatal("page.snap was not requested after failure")
	}
}

func TestMacroRecordPressAndScroll(t *testing.T) {
	cfg := testConfig(t)
	cfg.Paths.MacrosDir = filepath.Join(cfg.Paths.Home, "macros")
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	macroServeFakeExtension(t, ext, func(req protocol.Request) (any, *protocol.RPCError) {
		switch req.Method {
		case protocol.MethodActionPress:
			return protocol.ActionPressResult{Success: true, Key: "Enter"}, nil
		case protocol.MethodActionScroll:
			return protocol.ActionScrollResult{ScrolledPixels: 400}, nil
		default:
			return nil, &protocol.RPCError{Code: protocol.ErrMethodNotFound, Message: "unexpected method " + req.Method}
		}
	})

	post := func(body string) protocol.Response {
		t.Helper()
		_, data := postRPC(t, srv, token, body)
		var resp protocol.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	if resp := post(`{"jsonrpc":"2.0","id":"m1","method":"macro.start","params":{"name":"keys"}}`); resp.Error != nil {
		t.Fatalf("macro.start error: %+v", resp.Error)
	}
	if resp := post(`{"jsonrpc":"2.0","id":"a1","method":"action.press","params":{"key":"Enter","modifiers":["Meta"]}}`); resp.Error != nil {
		t.Fatalf("action.press error: %+v", resp.Error)
	}
	if resp := post(`{"jsonrpc":"2.0","id":"a2","method":"action.scroll","params":{"direction":"down","amount":400}}`); resp.Error != nil {
		t.Fatalf("action.scroll error: %+v", resp.Error)
	}
	if resp := post(`{"jsonrpc":"2.0","id":"s1","method":"macro.save","params":{}}`); resp.Error != nil {
		t.Fatalf("macro.save error: %+v", resp.Error)
	}

	loaded, err := macro.Load(cfg.Paths.MacrosDir, "keys")
	if err != nil {
		t.Fatalf("load macro: %v", err)
	}
	if len(loaded.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(loaded.Steps))
	}
	if loaded.Steps[0].Action != "press" || loaded.Steps[0].Value != "Enter" {
		t.Fatalf("press step = %+v", loaded.Steps[0])
	}
	if loaded.Steps[0].Options == nil || len(loaded.Steps[0].Options.Modifiers) != 1 || loaded.Steps[0].Options.Modifiers[0] != "Meta" {
		t.Fatalf("press options = %+v, want Meta", loaded.Steps[0].Options)
	}
	if loaded.Steps[1].Action != "scroll" || loaded.Steps[1].Options == nil {
		t.Fatalf("scroll step = %+v", loaded.Steps[1])
	}
	if loaded.Steps[1].Options.Direction != "down" || loaded.Steps[1].Options.Amount != 400 {
		t.Fatalf("scroll options = %+v", loaded.Steps[1].Options)
	}
}

func TestMacroRecordingIgnoresSnap(t *testing.T) {
	cfg := testConfig(t)
	cfg.Paths.MacrosDir = filepath.Join(cfg.Paths.Home, "macros")
	srv := startDaemon(t, cfg)
	token := testToken(t, cfg)
	ext := dialWS(t, wsURL(srv.URL, token))
	readReady(t, ext)

	macroServeFakeExtension(t, ext, func(req protocol.Request) (any, *protocol.RPCError) {
		switch req.Method {
		case protocol.MethodPageSnap:
			return protocol.SnapResult{FormattedText: "TAB [1] Example"}, nil
		default:
			return nil, &protocol.RPCError{Code: protocol.ErrMethodNotFound, Message: "unexpected method " + req.Method}
		}
	})

	_, data := postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"m1","method":"macro.start","params":{"name":"snaps"}}`)
	var resp protocol.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode macro.start: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("macro.start error: %+v", resp.Error)
	}

	_, data = postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"p1","method":"page.snap","params":{}}`)
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode page.snap: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("page.snap while recording error: %+v", resp.Error)
	}

	_, data = postRPC(t, srv, token, `{"jsonrpc":"2.0","id":"s1","method":"macro.save","params":{}}`)
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("decode macro.save: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("macro.save error: %+v", resp.Error)
	}
	var saved protocol.MacroSaveResult
	if err := json.Unmarshal(resp.Result, &saved); err != nil {
		t.Fatalf("decode macro.save result: %v", err)
	}
	if saved.Steps != 0 {
		t.Fatalf("saved steps = %d, want 0 (snap must not be recorded)", saved.Steps)
	}
	if saved.Parameters == nil {
		t.Fatal("parameters = null, want empty array")
	}
}
