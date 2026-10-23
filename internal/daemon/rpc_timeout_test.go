package daemon

import (
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestForwardTimeoutFor(t *testing.T) {
	fallback := 7 * time.Second
	tests := []struct {
		method string
		want   time.Duration
	}{
		{protocol.MethodActionType, actionTypeTimeout},
		{protocol.MethodActionClick, actionMediumTimeout},
		{protocol.MethodActionScroll, actionMediumTimeout},
		{protocol.MethodActionUpload, actionMediumTimeout},
		{protocol.MethodActionPress, actionPressTimeout},
		{protocol.MethodPageWait, pageWaitTimeout},
		{protocol.MethodPageSnap, fallback},
		{protocol.MethodSessionStatus, fallback},
	}

	for _, tt := range tests {
		if got := forwardTimeoutFor(tt.method, fallback); got != tt.want {
			t.Errorf("forwardTimeoutFor(%q) = %v, want %v", tt.method, got, tt.want)
		}
	}
}
