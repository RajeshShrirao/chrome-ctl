// Package client is a stateless JSON-RPC client for the local chrome-ctl daemon.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const maxResponseBytes = 1 << 20

type Client struct {
	addr  string
	token string
	http  *http.Client
}

func New(addr, token string) *Client {
	return &Client{addr: addr, token: token, http: &http.Client{}}
}

// Call posts a JSON-RPC request and returns either the raw result or the daemon's RPC error.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, *protocol.RPCError, error) {
	id, err := newRequestID()
	if err != nil {
		return nil, nil, err
	}

	req := protocol.Request{JSONRPC: protocol.JSONRPC, ID: &id, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, nil, fmt.Errorf("encode params: %w", err)
		}
		req.Params = raw
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.addr+"/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("call daemon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, nil, fmt.Errorf("daemon rejected the auth token (HTTP 401); check ~/.chrome-ctl/auth_token")
	}
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, nil, fmt.Errorf("daemon returned HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}

	var rpcResp protocol.Response
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, nil, fmt.Errorf("decode response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, rpcResp.Error, nil
	}
	return rpcResp.Result, nil, nil
}

func (c *Client) Status(ctx context.Context) (protocol.SessionStatus, error) {
	var status protocol.SessionStatus
	raw, rpcErr, err := c.Call(ctx, protocol.MethodSessionStatus, nil)
	if err != nil {
		return status, err
	}
	if rpcErr != nil {
		return status, fmt.Errorf("session.status failed: %s (code %d)", rpcErr.Message, rpcErr.Code)
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return status, fmt.Errorf("decode session.status result: %w", err)
	}
	return status, nil
}

func newRequestID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate request id: %w", err)
	}
	return "req_" + hex.EncodeToString(buf), nil
}
