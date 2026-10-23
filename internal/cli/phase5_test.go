package cli

import (
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestValidateSSOTimeout(t *testing.T) {
	for _, timeoutMs := range []int{1000, 15000, 60000} {
		if err := validateSSOTimeout(timeoutMs); err != nil {
			t.Errorf("validateSSOTimeout(%d) error = %v, want nil", timeoutMs, err)
		}
	}
	for _, timeoutMs := range []int{-1, 0, 999, 60001} {
		if err := validateSSOTimeout(timeoutMs); err == nil {
			t.Errorf("validateSSOTimeout(%d) error = nil, want error", timeoutMs)
		}
	}

	err := validateSSOTimeout(999)
	if err == nil || err.Error() != "timeout must be between 1000 and 60000 ms (got 999)" {
		t.Errorf("validateSSOTimeout(999) = %v, want range error", err)
	}
}

func TestFormatSSO(t *testing.T) {
	tests := []struct {
		name   string
		result protocol.AuthSSOResult
		want   string
	}{
		{
			name: "signed in",
			result: protocol.AuthSSOResult{
				Success:           true,
				SelectedAccount:   "user@example.com",
				RedirectCompleted: true,
				ParentTabURL:      "https://clutch.co/profile",
			},
			want: "OK: signed in as user@example.com (redirect complete)\n",
		},
		{
			name: "redirect without account",
			result: protocol.AuthSSOResult{
				Success:           true,
				RedirectCompleted: true,
			},
			want: "OK: OAuth completed (redirect complete)\n",
		},
		{
			name: "popup closed only",
			result: protocol.AuthSSOResult{
				Success:         true,
				SelectedAccount: "user@example.com",
			},
			want: "OK: OAuth popup closed (redirect pending)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSSO(tt.result); got != tt.want {
				t.Errorf("formatSSO() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapRPCErrorSSOTimeout(t *testing.T) {
	msg := "SSO timeout: no Google OAuth popup detected (is the account picker open?)"
	err := mapRPCError(&protocol.RPCError{Code: protocol.ErrSSOTimeout, Message: msg})
	if err == nil {
		t.Fatal("mapRPCError(ErrSSOTimeout) = nil, want error")
	}
	if err.Error() != msg {
		t.Errorf("mapRPCError(ErrSSOTimeout) = %q, want verbatim %q", err.Error(), msg)
	}
}

func TestParseMacroParams(t *testing.T) {
	got := parseMacroParams(" a, b ,,c ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("parseMacroParams() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseMacroParams() = %v, want %v", got, want)
		}
	}

	if got := parseMacroParams(""); got != nil {
		t.Errorf("parseMacroParams(\"\") = %v, want nil", got)
	}
	if got := parseMacroParams(" , "); got != nil {
		t.Errorf("parseMacroParams(\" , \") = %v, want nil", got)
	}
}

func TestFormatMacroSave(t *testing.T) {
	withParams := protocol.MacroSaveResult{
		Name:       "lead_search",
		Steps:      2,
		Parameters: []string{"search_term", "limit"},
	}
	if got, want := formatMacroSave(withParams), "OK: saved macro \"lead_search\" (2 steps, params: search_term, limit)\n"; got != want {
		t.Errorf("formatMacroSave(params) = %q, want %q", got, want)
	}

	withoutParams := protocol.MacroSaveResult{Name: "lead_search", Steps: 2}
	if got, want := formatMacroSave(withoutParams), "OK: saved macro \"lead_search\" (2 steps)\n"; got != want {
		t.Errorf("formatMacroSave() = %q, want %q", got, want)
	}
}

func TestFormatMacroList(t *testing.T) {
	if got, want := formatMacroList(nil), "No macros saved.\n"; got != want {
		t.Errorf("formatMacroList(nil) = %q, want %q", got, want)
	}

	macros := []protocol.MacroSummary{
		{Name: "lead_search", Steps: 2, Parameters: []string{"search_term", "limit"}, Description: "Search leads"},
		{Name: "params_only", Steps: 3, Parameters: []string{"search_term"}},
		{Name: "bare", Steps: 1},
	}
	want := "MACROS\n" +
		"lead_search - 2 steps [params: search_term, limit] - Search leads\n" +
		"params_only - 3 steps [params: search_term]\n" +
		"bare - 1 step\n"
	if got := formatMacroList(macros); got != want {
		t.Errorf("formatMacroList() = %q, want %q", got, want)
	}

	// Ordering is daemon-provided and must not be re-sorted.
	reversed := []protocol.MacroSummary{{Name: "zeta", Steps: 1}, {Name: "alpha", Steps: 1}}
	want = "MACROS\nzeta - 1 step\nalpha - 1 step\n"
	if got := formatMacroList(reversed); got != want {
		t.Errorf("formatMacroList(unsorted) = %q, want %q", got, want)
	}
}

func TestFormatMacroRun(t *testing.T) {
	result := protocol.MacroRunResult{
		Macro:      "lead_search",
		StepsRun:   2,
		DurationMs: 2450,
		Completed:  true,
	}
	if got, want := formatMacroRun(result), "OK: ran macro \"lead_search\" (2 steps, 2.5s)\n"; got != want {
		t.Errorf("formatMacroRun() = %q, want %q", got, want)
	}

	result.DurationMs = 1000
	if got, want := formatMacroRun(result), "OK: ran macro \"lead_search\" (2 steps, 1.0s)\n"; got != want {
		t.Errorf("formatMacroRun(1000ms) = %q, want %q", got, want)
	}
}

func TestFormatMacroStepFailure(t *testing.T) {
	failure := protocol.MacroStepFailure{
		Status:          "step_failed",
		Macro:           "lead_search",
		FailedStepID:    3,
		Reason:          "element not found",
		CurrentSnapshot: "button \"Search\"\n",
		RemainingSteps:  2,
	}
	want := "MACRO STEP FAILED: macro \"lead_search\" step 3: element not found\nbutton \"Search\"\n"
	if got := formatMacroStepFailure(failure); got != want {
		t.Errorf("formatMacroStepFailure() = %q, want %q", got, want)
	}

	failure.CurrentSnapshot = "button \"Search\""
	if got := formatMacroStepFailure(failure); got != want {
		t.Errorf("formatMacroStepFailure(no trailing newline) = %q, want %q", got, want)
	}

	failure.CurrentSnapshot = ""
	want = "MACRO STEP FAILED: macro \"lead_search\" step 3: element not found\n"
	if got := formatMacroStepFailure(failure); got != want {
		t.Errorf("formatMacroStepFailure(empty snapshot) = %q, want %q", got, want)
	}
}

func TestDecodeMacroStepFailure(t *testing.T) {
	// The daemon transports Data as decoded JSON, so it arrives as any.
	data := map[string]any{
		"status":           "step_failed",
		"macro":            "lead_search",
		"failed_step_id":   float64(4),
		"reason":           "click timed out",
		"current_snapshot": "input \"Search\"",
		"remaining_steps":  float64(1),
	}
	failure, ok := decodeMacroStepFailure(data)
	if !ok {
		t.Fatal("decodeMacroStepFailure() ok = false, want true")
	}
	if failure.Macro != "lead_search" || failure.FailedStepID != 4 || failure.RemainingSteps != 1 {
		t.Errorf("decodeMacroStepFailure() = %+v", failure)
	}

	if _, ok := decodeMacroStepFailure(nil); ok {
		t.Error("decodeMacroStepFailure(nil) ok = true, want false")
	}
	if _, ok := decodeMacroStepFailure("not an object"); ok {
		t.Error("decodeMacroStepFailure(string) ok = true, want false")
	}
}

func TestParseMacroVars(t *testing.T) {
	vars, err := parseMacroVars(`{"search_term": "Design Studios", "limit": "10"}`)
	if err != nil {
		t.Fatalf("parseMacroVars() error = %v", err)
	}
	if len(vars) != 2 || vars["search_term"] != "Design Studios" || vars["limit"] != "10" {
		t.Errorf("parseMacroVars() = %v", vars)
	}

	empty, err := parseMacroVars("{}")
	if err != nil {
		t.Fatalf("parseMacroVars({}) error = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("parseMacroVars({}) = %v, want empty non-nil map", empty)
	}

	for _, raw := range []string{``, `{`, `not json`, `{"limit": 10}`, `{"flag": true}`, `["a"]`} {
		if _, err := parseMacroVars(raw); err == nil {
			t.Errorf("parseMacroVars(%q) error = nil, want error", raw)
		}
	}
}

func TestPhase5CommandsRegistered(t *testing.T) {
	root := NewRoot()

	sso, _, err := root.Find([]string{"sso"})
	if err != nil || sso.Name() != "sso" {
		t.Fatalf("root.Find(sso) = %v, %v", sso, err)
	}

	macro, _, err := root.Find([]string{"macro"})
	if err != nil || macro.Name() != "macro" {
		t.Fatalf("root.Find(macro) = %v, %v", macro, err)
	}
	for _, sub := range []string{"start", "save", "list", "run"} {
		if found, _, err := macro.Find([]string{sub}); err != nil || found.Name() != sub {
			t.Errorf("macro.Find(%s) = %v, %v", sub, found, err)
		}
	}
}
