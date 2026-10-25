package cli

import (
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestMapRPCErrorDoesNotCallTimeoutsDisconnected(t *testing.T) {
	tests := []struct {
		name    string
		rpcErr  *protocol.RPCError
		want    []string
		notWant []string
	}{
		{
			name:    "dispatched but unconfirmed",
			rpcErr:  &protocol.RPCError{Code: protocol.ErrDispatchedUnconfirmed, Message: "request dispatched but no ack after 10s"},
			want:    []string{"DISPATCHED", "State unknown", "snap", "do not repeat"},
			notWant: []string{"not connected"},
		},
		{
			name:    "extension relay timeout",
			rpcErr:  &protocol.RPCError{Code: protocol.ErrInternal, Message: "extension timed out"},
			want:    []string{"DISPATCHED", "State unknown"},
			notWant: []string{"not connected"},
		},
		{
			name:    "genuinely disconnected",
			rpcErr:  &protocol.RPCError{Code: protocol.ErrConnection, Message: "chrome extension not connected"},
			want:    []string{"not connected", "chrome extension not connected"},
			notWant: []string{"DISPATCHED"},
		},
		{
			name:   "stale snapshot is verbatim",
			rpcErr: &protocol.RPCError{Code: protocol.ErrSnapshotStale, Message: "snapshot stale: page changed since snapshot 3; run snap"},
			want:   []string{"snapshot stale: page changed since snapshot 3"},
		},
		{
			name:   "feature disabled is verbatim",
			rpcErr: &protocol.RPCError{Code: protocol.ErrFeatureDisabled, Message: "page.eval is disabled; restart the daemon without --no-eval"},
			want:   []string{"restart the daemon without --no-eval"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapRPCError(tt.rpcErr)
			if err == nil {
				t.Fatal("error = nil")
			}
			msg := err.Error()
			for _, want := range tt.want {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want substring %q", msg, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(strings.ToLower(msg), notWant) {
					t.Errorf("error = %q, should not contain %q", msg, notWant)
				}
			}
		})
	}
}
