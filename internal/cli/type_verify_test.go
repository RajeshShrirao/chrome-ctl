package cli

import (
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestFormatTypeVerified(t *testing.T) {
	got := formatType(protocol.ActionTypeResult{
		CharsTyped:   12,
		DurationMs:   2500,
		Verified:     true,
		Verification: "exact",
		ReadBack:     "hello world!",
	}, 22)

	want := "OK: Typed 12 chars into [22] (verified; human cadence: 2.5s).\n"
	if got != want {
		t.Errorf("formatType(verified) = %q, want %q", got, want)
	}
}

func TestFormatTypeEmptyWarns(t *testing.T) {
	got := formatType(protocol.ActionTypeResult{
		CharsTyped:   12,
		DurationMs:   2500,
		Verification: "empty",
	}, 22)

	if !strings.HasPrefix(got, "WARN: Typed 12 chars into [22] but the field reads empty") {
		t.Errorf("formatType(empty) = %q, want a WARN line", got)
	}
	if strings.Contains(got, "verified") {
		t.Errorf("formatType(empty) claimed verification: %q", got)
	}
}

func TestFormatTypeMismatchShowsReadBack(t *testing.T) {
	got := formatType(protocol.ActionTypeResult{
		CharsTyped:   12,
		DurationMs:   2500,
		Verification: "mismatch",
		ReadBack:     "prefilled",
	}, 22)

	if !strings.Contains(got, `reads "prefilled"`) {
		t.Errorf("formatType(mismatch) = %q, want the read-back value", got)
	}
}

func TestFormatTypeTruncatesLongReadBack(t *testing.T) {
	got := formatType(protocol.ActionTypeResult{
		CharsTyped:   200,
		Verification: "mismatch",
		ReadBack:     strings.Repeat("x", 400),
	}, 7)

	if strings.Contains(got, strings.Repeat("x", 200)) {
		t.Errorf("formatType did not truncate the read-back: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("formatType should mark truncation with an ellipsis: %q", got)
	}
}

func TestFormatTypeUnverifiedWithoutVerificationStaysQuiet(t *testing.T) {
	got := formatType(protocol.ActionTypeResult{
		CharsTyped: 5,
		DurationMs: 800,
	}, 3)

	want := "OK: Typed 5 chars into [3] (human cadence: 0.8s).\n"
	if got != want {
		t.Errorf("formatType(unverified) = %q, want %q", got, want)
	}
}
