package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/client"
	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const rpcTimeout = 20 * time.Second

// newRPCClient loads the local auth token and builds a client for the daemon.
func newRPCClient() (*client.Client, error) {
	paths := config.DefaultPaths()
	token, err := config.ReadToken(paths.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read auth token from %s: %w; start the daemon with `chrome-ctl daemon --start`", paths.TokenFile, err)
	}
	return client.New(config.DefaultAddr(), token), nil
}

// callRPC performs one request with the default timeout and decodes its result into out.
func callRPC(cmd *cobra.Command, method string, params, out any) error {
	return callRPCWithTimeout(cmd, method, params, out, rpcTimeout)
}

// callRPCWithTimeout performs one request with an explicit timeout and decodes
// its result into out.
func callRPCWithTimeout(cmd *cobra.Command, method string, params, out any, timeout time.Duration) error {
	c, err := newRPCClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	raw, rpcErr, err := c.Call(ctx, method, params)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if rpcErr != nil {
		return mapRPCError(rpcErr)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s result: %w", method, err)
	}
	return nil
}

// rpcMappedError keeps the protocol code alongside the user-facing text so
// callers (for example bulk retries) can branch on data, never on message text.
type rpcMappedError struct {
	code    int
	message string
	data    any
	display string
}

func (e *rpcMappedError) Error() string { return e.display }

// rpcErrorCode returns the protocol code carried by a mapped error.
func rpcErrorCode(err error) (int, bool) {
	var mapped *rpcMappedError
	if errors.As(err, &mapped) {
		return mapped.code, true
	}
	return 0, false
}

func mapRPCError(rpcErr *protocol.RPCError) error {
	display := rpcErr.Message
	switch rpcErr.Code {
	case protocol.ErrConnection:
		display = fmt.Sprintf("Chrome extension not connected (daemon reported: %s)", rpcErr.Message)
	case protocol.ErrDispatchedUnconfirmed:
		display = fmt.Sprintf("DISPATCHED, %s. State unknown — run `chrome-ctl snap` before retrying; do not repeat the action blindly", rpcErr.Message)
	case protocol.ErrSnapshotStale, protocol.ErrFeatureDisabled:
		display = rpcErr.Message
	case protocol.ErrElementNotFound, protocol.ErrElementNotInteractable, protocol.ErrTabNotFound,
		protocol.ErrDebuggerDetached, protocol.ErrNavigationFailed,
		protocol.ErrSSOTimeout:
		// These carry actionable messages from the extension; show them verbatim.
		display = rpcErr.Message
	case protocol.ErrInternal:
		// The offscreen relay reports its own deadline this way. The request
		// was already dispatched, so the side effect may have occurred.
		if rpcErr.Message == "extension timed out" {
			display = "DISPATCHED, the extension stopped waiting for an ack. State unknown — run `chrome-ctl snap` before retrying; do not repeat the action blindly"
		} else {
			display = fmt.Sprintf("%s (code %d)", rpcErr.Message, rpcErr.Code)
		}
	default:
		display = fmt.Sprintf("%s (code %d)", rpcErr.Message, rpcErr.Code)
	}
	return &rpcMappedError{
		code:    rpcErr.Code,
		message: rpcErr.Message,
		data:    rpcErr.Data,
		display: display,
	}
}
