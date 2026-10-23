package protocol

import "encoding/json"

const JSONRPC = "2.0"

const (
	MethodSystemReady   = "system.ready"
	MethodSystemPing    = "system.ping"
	MethodSessionStatus = "session.status"
)

const (
	ErrConnection     = -32000
	ErrMethodNotFound = -32601
	ErrInvalidRequest = -32600
	ErrInternal       = -32603
	ErrUnauthorized   = -32006
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type ActiveTab struct {
	TabID int    `json:"tabId"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type ExtensionStatus struct {
	ActiveTab        *ActiveTab `json:"activeTab"`
	TabCount         int        `json:"tabCount"`
	DebuggerAttached bool       `json:"debuggerAttached"`
}

type SessionStatus struct {
	Status             string     `json:"status"`
	ExtensionConnected bool       `json:"extensionConnected"`
	Version            string     `json:"version"`
	UptimeSec          int64      `json:"uptimeSec"`
	ActiveTab          *ActiveTab `json:"activeTab"`
	TabCount           int        `json:"tabCount"`
	DebuggerAttached   bool       `json:"debuggerAttached"`
}

type SystemReadyParams struct {
	Version         string `json:"version"`
	DaemonUptimeSec int64  `json:"daemonUptimeSec"`
	AttachedTabs    []int  `json:"attachedTabs"`
}

type PingParams struct {
	Timestamp int64 `json:"timestamp"`
}

type PongResult struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

func Success(id *string, result any) Response {
	raw, err := json.Marshal(result)
	if err != nil {
		return Failure(id, ErrInternal, "failed to encode result")
	}
	return Response{JSONRPC: JSONRPC, ID: id, Result: raw}
}

func Failure(id *string, code int, message string) Response {
	return Response{
		JSONRPC: JSONRPC,
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}
