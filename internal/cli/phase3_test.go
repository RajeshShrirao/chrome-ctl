package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestFormatClick(t *testing.T) {
	base := protocol.ActionClickResult{
		Success:     true,
		Action:      "click",
		Target:      &protocol.ActionTarget{ID: 2, Tag: "button", Label: "Start a post"},
		Coordinates: &protocol.ActionCoordinates{X: 542, Y: 138},
		DurationMs:  340,
	}

	if got, want := formatClick(base, false), "OK: clicked [2] <button> \"Start a post\" @542,138 (0.34s)\n"; got != want {
		t.Errorf("formatClick() = %q, want %q", got, want)
	}

	if got, want := formatClick(base, true), "OK: double-clicked [2] <button> \"Start a post\" @542,138 (0.34s)\n"; got != want {
		t.Errorf("formatClick(double) = %q, want %q", got, want)
	}

	withSnap := base
	withSnap.Snapshot = &protocol.SnapResult{FormattedText: "<button> Start a post\n\n"}
	want := "OK: clicked [2] <button> \"Start a post\" @542,138 (0.34s)\n\n<button> Start a post\n"
	if got := formatClick(withSnap, false); got != want {
		t.Errorf("formatClick(snapshot) = %q, want %q", got, want)
	}
}

func TestFormatType(t *testing.T) {
	base := protocol.ActionTypeResult{Success: true, CharsTyped: 36, DurationMs: 2500}
	if got, want := formatType(base, 4), "OK: Typed 36 chars into [4] (human cadence: 2.5s).\n"; got != want {
		t.Errorf("formatType() = %q, want %q", got, want)
	}

	oneTypo := base
	oneTypo.TyposInjected = 1
	if got, want := formatType(oneTypo, 4), "OK: Typed 36 chars into [4] (human cadence: 2.5s, 1 typo).\n"; got != want {
		t.Errorf("formatType(1 typo) = %q, want %q", got, want)
	}

	twoTypos := base
	twoTypos.TyposInjected = 2
	if got, want := formatType(twoTypos, 4), "OK: Typed 36 chars into [4] (human cadence: 2.5s, 2 typos).\n"; got != want {
		t.Errorf("formatType(2 typos) = %q, want %q", got, want)
	}

	withDelta := oneTypo
	withDelta.StateDelta = "value: (empty) -> hello"
	want := "OK: Typed 36 chars into [4] (human cadence: 2.5s, 1 typo). State changed: value: (empty) -> hello.\n"
	if got := formatType(withDelta, 4); got != want {
		t.Errorf("formatType(state delta) = %q, want %q", got, want)
	}

	withSnap := base
	withSnap.Snapshot = &protocol.SnapResult{FormattedText: "line1\nline2\n\n"}
	want = "OK: Typed 36 chars into [4] (human cadence: 2.5s).\n\nline1\nline2\n"
	if got := formatType(withSnap, 4); got != want {
		t.Errorf("formatType(snapshot) = %q, want %q", got, want)
	}
}

func TestFormatPress(t *testing.T) {
	result := protocol.ActionPressResult{Success: true, Key: "Enter"}
	if got, want := formatPress(result, nil), "OK: pressed Enter\n"; got != want {
		t.Errorf("formatPress() = %q, want %q", got, want)
	}

	if got, want := formatPress(result, []string{"Ctrl", "Shift"}), "OK: pressed Ctrl+Shift+Enter\n"; got != want {
		t.Errorf("formatPress(mods) = %q, want %q", got, want)
	}
}

func TestFormatScroll(t *testing.T) {
	result := protocol.ActionScrollResult{ScrolledPixels: 720, NewScrollY: 720, ViewportPercent: 17}
	if got, want := formatScroll(result, "down"), "OK: scrolled down 720px to 720px (17%)\n"; got != want {
		t.Errorf("formatScroll() = %q, want %q", got, want)
	}

	floatResult := protocol.ActionScrollResult{ScrolledPixels: 719.6, NewScrollY: 720.4, ViewportPercent: 16.8}
	if got, want := formatScroll(floatResult, "up"), "OK: scrolled up 720px to 720px (17%)\n"; got != want {
		t.Errorf("formatScroll(floatResult) = %q, want %q", got, want)
	}
}

func TestFormatBytesAndUpload(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{45158, "44.1 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024, "5.0 MB"},
	}
	for _, tt := range tests {
		if got := formatBytes(tt.n); got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}

	if got, want := formatUpload("logo.png", 45158, 5), "OK: uploaded logo.png (44.1 KB) to [5]\n"; got != want {
		t.Errorf("formatUpload() = %q, want %q", got, want)
	}
}

func TestFormatWaitDuration(t *testing.T) {
	tests := []struct {
		ms   int
		want string
	}{
		{0, "0ms"},
		{250, "250ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1200, "1.2s"},
		{2500, "2.5s"},
		{10000, "10.0s"},
		{600000, "600.0s"},
	}
	for _, tt := range tests {
		if got := formatWaitDuration(tt.ms); got != tt.want {
			t.Errorf("formatWaitDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestParseActionID(t *testing.T) {
	if id, err := parseActionID("42"); err != nil || id != 42 {
		t.Errorf("parseActionID(42) = %d, %v; want 42, nil", id, err)
	}
	if id, err := parseActionID(" 7 "); err != nil || id != 7 {
		t.Errorf("parseActionID(' 7 ') = %d, %v; want 7, nil", id, err)
	}
	for _, arg := range []string{"0", "-1", "abc", "", "1.5"} {
		if _, err := parseActionID(arg); err == nil {
			t.Errorf("parseActionID(%q) error = nil, want error", arg)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"enter", "Enter"},
		{"PAGEUP", "PageUp"},
		{"ArrowLeft", "ArrowLeft"},
		{"escape", "Escape"},
		{"x", "x"},
		{"!", "!"},
	}
	for _, tt := range tests {
		got, err := normalizeKey(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("normalizeKey(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}

	for _, in := range []string{"F13", "", "ab", "page_up"} {
		if _, err := normalizeKey(in); err == nil {
			t.Errorf("normalizeKey(%q) error = nil, want error", in)
		}
	}
}

func TestNormalizeModifiers(t *testing.T) {
	got, err := normalizeModifiers([]string{"control", "ALT", "Shift", "cmd", "command", "meta"})
	if err != nil {
		t.Fatalf("normalizeModifiers() error = %v", err)
	}
	want := []string{"Ctrl", "Alt", "Shift", "Meta", "Meta", "Meta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("normalizeModifiers() = %v, want %v", got, want)
	}
	if _, err := normalizeModifiers([]string{"hyper"}); err == nil {
		t.Error("normalizeModifiers(hyper) error = nil, want error")
	}
}

func TestParseScrollArgs(t *testing.T) {
	params, err := parseScrollArgs([]string{"DOWN"})
	if err != nil {
		t.Fatalf("parseScrollArgs() error = %v", err)
	}
	if params.Direction != "down" || params.Amount != 0.8 {
		t.Errorf("parseScrollArgs() = %+v, want down/0.8", params)
	}

	params, err = parseScrollArgs([]string{"up", "2.5"})
	if err != nil {
		t.Fatalf("parseScrollArgs(amount) error = %v", err)
	}
	if params.Direction != "up" || params.Amount != 2.5 {
		t.Errorf("parseScrollArgs(amount) = %+v, want up/2.5", params)
	}

	if _, err := parseScrollArgs([]string{"left"}); err == nil {
		t.Error("parseScrollArgs(left) error = nil, want error")
	}
	for _, amount := range []string{"0", "-1", "10.1", "abc", "NaN"} {
		if _, err := parseScrollArgs([]string{"down", amount}); err == nil {
			t.Errorf("parseScrollArgs(down, %q) error = nil, want error", amount)
		}
	}
	if _, err := parseScrollArgs([]string{"down", "10"}); err != nil {
		t.Errorf("parseScrollArgs(down, 10) error = %v, want nil", err)
	}
}

func TestParseWaitArg(t *testing.T) {
	spec, err := parseWaitArg("250")
	if err != nil || spec.mode != waitSleep || spec.minMs != 250 || spec.maxMs != 250 {
		t.Errorf("parseWaitArg(250) = %+v, %v; want sleep 250", spec, err)
	}

	spec, err = parseWaitArg("3000-7000")
	if err != nil || spec.mode != waitSleep || spec.minMs != 3000 || spec.maxMs != 7000 {
		t.Errorf("parseWaitArg(3000-7000) = %+v, %v; want sleep 3000-7000", spec, err)
	}

	for _, arg := range []string{"idle", "IDLE"} {
		spec, err = parseWaitArg(arg)
		if err != nil || spec.mode != waitIdle {
			t.Errorf("parseWaitArg(%q) = %+v, %v; want idle", arg, spec, err)
		}
	}

	spec, err = parseWaitArg("main .content")
	if err != nil || spec.mode != waitSelector || spec.selector != "main .content" {
		t.Errorf("parseWaitArg(selector) = %+v, %v; want selector", spec, err)
	}

	spec, err = parseWaitArg("main-content")
	if err != nil || spec.mode != waitSelector || spec.selector != "main-content" {
		t.Errorf("parseWaitArg(hyphenated selector) = %+v, %v; want selector", spec, err)
	}

	if _, err := parseWaitArg("700001"); err == nil {
		t.Error("parseWaitArg(700001) error = nil, want out of range error")
	}
	if _, err := parseWaitArg("7000-3000"); err == nil {
		t.Error("parseWaitArg(7000-3000) error = nil, want inverted range error")
	}
	if _, err := parseWaitArg("-5"); err == nil {
		t.Error("parseWaitArg(-5) error = nil, want error")
	}
	if _, err := parseWaitArg(""); err == nil {
		t.Error("parseWaitArg(empty) error = nil, want error")
	}
}

func TestSleepWithContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepWithContext(ctx, time.Hour); err == nil {
		t.Error("sleepWithContext(canceled) error = nil, want context error")
	}
}

func TestClickCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(
		`{"success":true,"action":"click","target":{"id":2,"tag":"button","label":"Start a post"},`+
			`"coordinates":{"x":542,"y":138},"durationMs":340,`+
			`"snapshot":{"formattedText":"<button> Start a post\n\n"}}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "click", "2", "--double", "--snap")
	if err != nil {
		t.Fatalf("click error = %v", err)
	}
	want := "OK: double-clicked [2] <button> \"Start a post\" @542,138 (0.34s)\n\n<button> Start a post\n"
	if out != want {
		t.Errorf("click output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodActionClick {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodActionClick)
	}
	var params protocol.ActionClickParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.ID != 2 || !params.DoubleClick || !params.Snap || params.TabID != nil {
		t.Errorf("params = %+v, want ID 2, double, snap", params)
	}
}

func TestClickCommandRejectsInvalidID(t *testing.T) {
	for _, arg := range []string{"0", "-1", "   "} {
		if _, err := executePhase2(t, "click", arg); err == nil {
			t.Errorf("click %q error = nil, want error", arg)
		}
	}
}

func TestClickCommandWithConsequenceAndText(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(
		`{"success":true,"action":"click","target":{"id":41,"tag":"input","label":"CPU"},`+
			`"coordinates":{"x":320,"y":410},"consequence":"selected (•)","durationMs":50,`+
			`"dispatchMs":40,"settleMs":10}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "click", "CPU")
	if err != nil {
		t.Fatalf("click CPU error = %v", err)
	}
	want := "OK: clicked [41] <input> \"CPU\" @320,410 -> selected (•) (0.05s: dispatch 0.04s, DOM settle 0.01s)\n"
	if out != want {
		t.Errorf("click output = %q, want %q", out, want)
	}

	req := <-requests
	var params protocol.ActionClickParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.Text != "CPU" {
		t.Errorf("params.Text = %q, want CPU", params.Text)
	}
}

func TestTypeCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(
		`{"success":true,"charsTyped":5,"durationMs":2500,"typosInjected":1,`+
			`"stateDelta":"value: (empty) -> hello"}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "type", "4", "hello", "--enter")
	if err != nil {
		t.Fatalf("type error = %v", err)
	}
	want := "OK: Typed 5 chars into [4] (human cadence: 2.5s, 1 typo). State changed: value: (empty) -> hello.\n"
	if out != want {
		t.Errorf("type output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodActionType {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodActionType)
	}
	var params protocol.ActionTypeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.ID != 4 || params.Text != "hello" || !params.Enter || params.Snap {
		t.Errorf("params = %+v, want ID 4, text hello, enter", params)
	}
	if params.TypoRate == nil || *params.TypoRate != 0.02 {
		t.Errorf("params.TypoRate = %v, want 0.02", params.TypoRate)
	}
}

func TestTypeCommandCustomRateAndSnap(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(
		`{"success":true,"charsTyped":3,"durationMs":1000,`+
			`"snapshot":{"formattedText":"line1\n"}}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "type", "9", "abc", "--typo-rate", "0.1", "--snap")
	if err != nil {
		t.Fatalf("type error = %v", err)
	}
	want := "OK: Typed 3 chars into [9] (human cadence: 1.0s).\n\nline1\n"
	if out != want {
		t.Errorf("type output = %q, want %q", out, want)
	}

	req := <-requests
	var params protocol.ActionTypeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.TypoRate == nil || *params.TypoRate != 0.1 || !params.Snap || params.Enter {
		t.Errorf("params = %+v, want typoRate 0.1 and snap", params)
	}
}

func TestTypeCommandValidation(t *testing.T) {
	setupStatusEnv(t)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bad id", []string{"type", "0", "hello"}, "positive integer"},
		{"empty text", []string{"type", "4", ""}, "text is empty"},
		{"too long", []string{"type", "4", strings.Repeat("a", 2001)}, "too long"},
		{"typo rate too high", []string{"type", "4", "hello", "--typo-rate", "1.5"}, "typo-rate"},
		{"typo rate negative", []string{"type", "4", "hello", "--typo-rate", "-0.1"}, "typo-rate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executePhase2(t, tt.args...)
			if err == nil {
				t.Fatalf("error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestTypeCommandEmptyTextWithEnter(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"success":true,"charsTyped":0,"durationMs":100}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "type", "4", "", "--enter")
	if err != nil {
		t.Fatalf("type empty text --enter error = %v, want nil", err)
	}
	if want := "OK: Typed 0 chars into [4] (human cadence: 0.1s).\n"; out != want {
		t.Errorf("type empty text output = %q, want %q", out, want)
	}
}

func TestPressCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(`{"success":true,"key":"Enter"}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "press", "enter", "--modifier", "control", "--modifier", "SHIFT")
	if err != nil {
		t.Fatalf("press error = %v", err)
	}
	if want := "OK: pressed Ctrl+Shift+Enter\n"; out != want {
		t.Errorf("press output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodActionPress {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodActionPress)
	}
	var params protocol.ActionPressParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.Key != "Enter" || strings.Join(params.Modifiers, ",") != "Ctrl,Shift" {
		t.Errorf("params = %+v, want Enter with Ctrl,Shift", params)
	}
}

func TestPressCommandValidation(t *testing.T) {
	setupStatusEnv(t)
	if _, err := executePhase2(t, "press", "F13"); err == nil {
		t.Error("press F13 error = nil, want error")
	}
	if _, err := executePhase2(t, "press", "enter", "--modifier", "hyper"); err == nil {
		t.Error("press --modifier hyper error = nil, want error")
	}
}

func TestScrollCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token,
		phase2Result(`{"scrolledPixels":720,"newScrollY":720,"viewportPercent":17}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "scroll", "DOWN")
	if err != nil {
		t.Fatalf("scroll error = %v", err)
	}
	if want := "OK: scrolled down 720px to 720px (17%)\n"; out != want {
		t.Errorf("scroll output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodActionScroll {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodActionScroll)
	}
	var params protocol.ActionScrollParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.Direction != "down" || params.Amount != 0.8 {
		t.Errorf("params = %+v, want down/0.8", params)
	}
}

func TestScrollCommandValidation(t *testing.T) {
	setupStatusEnv(t)
	for _, args := range [][]string{
		{"scroll", "left"},
		{"scroll", "down", "0"},
		{"scroll", "down", "11"},
		{"scroll", "down", "abc"},
	} {
		if _, err := executePhase2(t, args...); err == nil {
			t.Errorf("%v error = nil, want error", args)
		}
	}
}

func TestUploadCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 45158), 0o600); err != nil {
		t.Fatal(err)
	}

	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token,
		phase2Result(`{"success":true,"fileName":"logo.png","fileSize":0}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	// Include a ".." component to confirm the CLI sends a cleaned absolute path.
	arg := filepath.Join(dir, "sub", "..", "logo.png")
	out, err := executePhase2(t, "upload", "5", arg)
	if err != nil {
		t.Fatalf("upload error = %v", err)
	}
	if want := "OK: uploaded logo.png (44.1 KB) to [5]\n"; out != want {
		t.Errorf("upload output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodActionUpload {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodActionUpload)
	}
	var params protocol.ActionUploadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.ID != 5 {
		t.Errorf("params.ID = %d, want 5", params.ID)
	}
	if params.FilePath != path || !filepath.IsAbs(params.FilePath) {
		t.Errorf("params.FilePath = %q, want absolute %q", params.FilePath, path)
	}
}

func TestUploadCommandValidation(t *testing.T) {
	setupStatusEnv(t)

	if _, err := executePhase2(t, "upload", "5", filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Error("upload missing file error = nil, want error")
	}
	if _, err := executePhase2(t, "upload", "5", t.TempDir()); err == nil {
		t.Error("upload directory error = nil, want error")
	}
	if _, err := executePhase2(t, "upload", "0", "anything"); err == nil {
		t.Error("upload invalid id error = nil, want error")
	}
}

func TestWaitCommandLocalSleep(t *testing.T) {
	out, err := executePhase2(t, "wait", "0")
	if err != nil {
		t.Fatalf("wait 0 error = %v", err)
	}
	if want := "OK: waited 0ms\n"; out != want {
		t.Errorf("wait 0 output = %q, want %q", out, want)
	}

	out, err = executePhase2(t, "wait", "0-1")
	if err != nil {
		t.Fatalf("wait 0-1 error = %v", err)
	}
	if out != "OK: waited 0ms\n" && out != "OK: waited 1ms\n" {
		t.Errorf("wait 0-1 output = %q, want 0ms or 1ms", out)
	}
}

func TestWaitCommandIdleMatched(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":1200,"matched":true}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "wait", "idle")
	if err != nil {
		t.Fatalf("wait idle error = %v", err)
	}
	if want := "OK: idle after 1.2s\n"; out != want {
		t.Errorf("wait idle output = %q, want %q", out, want)
	}

	req := <-requests
	if req.Method != protocol.MethodPageWait {
		t.Errorf("method = %q, want %q", req.Method, protocol.MethodPageWait)
	}
	var params protocol.PageWaitParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if !params.Idle || params.TimeoutMs != 10000 || params.Selector != "" {
		t.Errorf("params = %+v, want idle with 10000ms timeout", params)
	}
}

func TestWaitCommandIdleTimeout(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":10000,"matched":false}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "wait", "idle")
	if err != nil {
		t.Fatalf("wait idle timeout error = %v, want nil", err)
	}
	if want := "WARN: DOM idle wait timed out after 10.0s; the page may still be fetching. Use wait --network or wait --for-text.\n"; out != want {
		t.Errorf("wait idle timeout output = %q, want %q", out, want)
	}
}

func TestWaitCommandSelectorMatched(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":1200,"matched":true}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "wait", "main")
	if err != nil {
		t.Fatalf("wait selector error = %v", err)
	}
	if want := "OK: selector \"main\" appeared after 1.2s\n"; out != want {
		t.Errorf("wait selector output = %q, want %q", out, want)
	}

	req := <-requests
	var params protocol.PageWaitParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.Selector != "main" || params.Idle || params.TimeoutMs != 10000 {
		t.Errorf("params = %+v, want selector main with 10000ms timeout", params)
	}
}

func TestWaitCommandSelectorTimeout(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakePhase2Server(t, token, phase2Result(`{"waitedMs":10000,"matched":false}`), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "wait", "main")
	if err == nil {
		t.Fatal("wait selector timeout error = nil, want error")
	}
	if want := "timed out after 10.0s waiting for \"main\""; err.Error() != want {
		t.Errorf("wait selector timeout error = %q, want %q", err, want)
	}
	if out != "" {
		t.Errorf("wait selector timeout output = %q, want empty", out)
	}
}

func TestWaitCommandValidation(t *testing.T) {
	setupStatusEnv(t)
	for _, arg := range []string{"700001", "7000-3000", "-5"} {
		if _, err := executePhase2(t, "wait", arg); err == nil {
			t.Errorf("wait %q error = nil, want error", arg)
		}
	}
}

func TestMapRPCErrorActionCodes(t *testing.T) {
	tests := []struct {
		code    int
		message string
	}{
		{protocol.ErrElementNotFound, "element [4] not found; run snap first"},
		{protocol.ErrElementNotInteractable, "element [4] is not interactable"},
		{protocol.ErrTabNotFound, "tab [9] not found"},
		{protocol.ErrDebuggerDetached, "debugger detached; retry"},
		{protocol.ErrNavigationFailed, "navigation failed"},
	}
	for _, tt := range tests {
		err := mapRPCError(&protocol.RPCError{Code: tt.code, Message: tt.message})
		if err == nil || err.Error() != tt.message {
			t.Errorf("mapRPCError(%d) = %v, want raw message %q", tt.code, err, tt.message)
		}
	}

	err := mapRPCError(&protocol.RPCError{Code: protocol.ErrInternal, Message: "boom"})
	if want := "boom (code -32603)"; err == nil || err.Error() != want {
		t.Errorf("mapRPCError(internal) = %v, want %q", err, want)
	}
}

func TestSeedFlagsRegistered(t *testing.T) {
	root := NewRoot()
	for _, name := range []string{"click", "clickat", "type", "press", "scroll"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("root.Find(%s) = %v", name, err)
		}
		if cmd.Flags().Lookup("seed") == nil {
			t.Errorf("%s missing --seed flag", name)
		}
	}
}
