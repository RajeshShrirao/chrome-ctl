package macro

import "github.com/rajeshshrirao/chrome-ctl/internal/protocol"

// Matches reports whether the observed effect satisfies every field the
// expectation asserts. Unset expectation fields are ignored.
func Matches(want protocol.MacroEffect, got protocol.MacroEffect) bool {
	if want.URL != "" && want.URL != got.URL {
		return false
	}
	if want.Value != "" && want.Value != got.Value {
		return false
	}
	if want.Checked != nil && (got.Checked == nil || *want.Checked != *got.Checked) {
		return false
	}
	if want.Disabled != nil && (got.Disabled == nil || *want.Disabled != *got.Disabled) {
		return false
	}
	if want.TargetCount != nil && (got.TargetCount == nil || *want.TargetCount != *got.TargetCount) {
		return false
	}
	return true
}

// Changed reports whether the observable signature moved between two samples.
func Changed(before protocol.MacroEffect, after protocol.MacroEffect) bool {
	if before.URL != after.URL || before.Value != after.Value {
		return true
	}
	if !equalBool(before.Checked, after.Checked) || !equalBool(before.Disabled, after.Disabled) {
		return true
	}
	return !equalInt(before.TargetCount, after.TargetCount)
}

func equalBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func equalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
