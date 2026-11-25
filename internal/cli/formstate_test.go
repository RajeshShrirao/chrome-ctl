package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestFormatFormControl(t *testing.T) {
	tests := []struct {
		name     string
		input    protocol.FormControlState
		expected string
	}{
		{
			name: "radio checked in subframe",
			input: protocol.FormControlState{
				Frame:   "f2",
				Tag:     "input",
				Type:    "radio",
				Name:    "q1",
				ID:      "opt1",
				Value:   "B",
				Checked: true,
				Label:   "CPU",
			},
			expected: `[f2] (•) <input:radio> name="q1" id="opt1" value="B" label="CPU"`,
		},
		{
			name: "checkbox unchecked in top doc",
			input: protocol.FormControlState{
				Frame:   "top",
				Tag:     "input",
				Type:    "checkbox",
				Name:    "terms",
				ID:      "agree",
				Checked: false,
				Label:   "I agree",
			},
			expected: `[top] [ ] <input:checkbox> name="terms" id="agree" label="I agree"`,
		},
		{
			name: "disabled button",
			input: protocol.FormControlState{
				Frame:    "f1",
				Tag:      "button",
				Type:     "button",
				ID:       "submit-btn",
				Disabled: true,
				Label:    "Submit",
			},
			expected: `[f1] <button> id="submit-btn" disabled label="Submit"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFormControl(tt.input)
			if got != tt.expected {
				t.Errorf("formatFormControl() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormStateCommand(t *testing.T) {
	_, token := setupStatusEnv(t)
	mockResponse := `{"controls":[
		{"frame":"top","tag":"input","type":"text","name":"search","value":"golang"},
		{"frame":"f1","tag":"input","type":"radio","name":"q1","value":"A","checked":true,"label":"CPU"}
	]}`
	srv := fakePhase2Server(t, token, phase2Result(mockResponse), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "formstate")
	if err != nil {
		t.Fatalf("formstate error = %v", err)
	}
	if !strings.Contains(out, `[top] <input:text> name="search" value="golang"`) {
		t.Errorf("expected search control in output, got: %s", out)
	}
	if !strings.Contains(out, `[f1] (•) <input:radio> name="q1" value="A" label="CPU"`) {
		t.Errorf("expected radio control in output, got: %s", out)
	}

	// Filter by frame
	filteredOut, err := executePhase2(t, "formstate", "--frame", "f1")
	if err != nil {
		t.Fatalf("formstate --frame error = %v", err)
	}
	if strings.Contains(filteredOut, "[top]") {
		t.Errorf("expected no top frame controls with --frame f1, got: %s", filteredOut)
	}
	if !strings.Contains(filteredOut, "[f1]") {
		t.Errorf("expected f1 controls with --frame f1, got: %s", filteredOut)
	}
}

func TestClickAtShotCoordinateConversion(t *testing.T) {
	dir := t.TempDir()
	shotPath := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(shotPath, []byte("fake png"), 0644); err != nil {
		t.Fatal(err)
	}

	metaJSON := `{"viewportWidth":1414,"viewportHeight":867,"imageWidth":1024,"imageHeight":628,"scale":0.7241867}`
	if err := os.WriteFile(shotPath+".meta.json", []byte(metaJSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Image coordinates 100, 200 should be scaled up by ~ 1414/1024 = 1.380859
	// 100 * (1414/1024) = 138.08 -> round 138
	// 200 * (867/628) = 276.11 -> round 276
	vx, vy, err := convertShotCoordinates(shotPath, 100, 200)
	if err != nil {
		t.Fatalf("convertShotCoordinates error: %v", err)
	}
	if vx != 138 || vy != 276 {
		t.Errorf("got vx=%d, vy=%d; want 138, 276", vx, vy)
	}
}

func TestClickAtArgParsing(t *testing.T) {
	// Comma separated
	x, y, err := parseCoordinates([]string{"65,238"})
	if err != nil || x != 65 || y != 238 {
		t.Errorf("comma separated parse failed: x=%d, y=%d, err=%v", x, y, err)
	}

	// Space separated
	x, y, err = parseCoordinates([]string{"65", "238"})
	if err != nil || x != 65 || y != 238 {
		t.Errorf("space separated parse failed: x=%d, y=%d, err=%v", x, y, err)
	}
}

func TestFormStateCommandFilled(t *testing.T) {
	_, token := setupStatusEnv(t)
	mockResponse := `{"controls":[
		{"frame":"top","tag":"input","type":"text","name":"empty"},
		{"frame":"top","tag":"input","type":"text","name":"spaces","value":"   \t"},
		{"frame":"top","tag":"input","type":"text","name":"filled_text","value":"alice"},
		{"frame":"top","tag":"input","type":"checkbox","name":"unchecked_box","checked":false},
		{"frame":"top","tag":"input","type":"checkbox","name":"checked_box","checked":true},
		{"frame":"f1","tag":"input","type":"radio","name":"choice","value":"B","checked":true}
	]}`
	srv := fakePhase2Server(t, token, phase2Result(mockResponse), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "formstate", "--filled")
	if err != nil {
		t.Fatalf("formstate --filled error = %v", err)
	}
	if strings.Contains(out, `name="empty"`) || strings.Contains(out, `name="spaces"`) || strings.Contains(out, `name="unchecked_box"`) {
		t.Errorf("expected empty/unchecked controls to be filtered out, got: %s", out)
	}
	if !strings.Contains(out, `name="filled_text"`) || !strings.Contains(out, `name="checked_box"`) || !strings.Contains(out, `name="choice"`) {
		t.Errorf("expected filled/checked controls to remain, got: %s", out)
	}

	jsonOut, err := executePhase2(t, "formstate", "--filled", "--frame", "missing", "--json")
	if err != nil {
		t.Fatalf("formstate --filled --json error = %v", err)
	}
	if strings.TrimSpace(jsonOut) != "[]" {
		t.Errorf("expected empty JSON array [], got: %q", jsonOut)
	}
}

func TestFormStateLabelAndValueTruncationVsFull(t *testing.T) {
	_, token := setupStatusEnv(t)
	longLabel := strings.Repeat("é", 130)
	longValue := strings.Repeat("界", 210)
	mockResponse := fmt.Sprintf(
		`{"controls":[{"frame":"top","tag":"input","type":"text","name":"bio","value":%q,"label":%q}]}`,
		longValue, longLabel,
	)
	srv := fakePhase2Server(t, token, phase2Result(mockResponse), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "formstate")
	if err != nil {
		t.Fatalf("formstate error = %v", err)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("truncated output is not valid UTF-8: %q", out)
	}
	wantLabel := fmt.Sprintf(`label=%q`, strings.Repeat("é", 120)+"…")
	wantValue := fmt.Sprintf(`value=%q`, strings.Repeat("界", 200)+"…")
	if !strings.Contains(out, wantLabel) {
		t.Errorf("expected truncated 120-rune label %q, got: %s", wantLabel, out)
	}
	if !strings.Contains(out, wantValue) {
		t.Errorf("expected truncated 200-rune value %q, got: %s", wantValue, out)
	}
	if strings.Contains(out, longLabel) || strings.Contains(out, longValue) {
		t.Errorf("expected default formstate to truncate label and value, got: %s", out)
	}

	fullOut, err := executePhase2(t, "formstate", "--full")
	if err != nil {
		t.Fatalf("formstate --full error = %v", err)
	}
	if !strings.Contains(fullOut, fmt.Sprintf(`label=%q`, longLabel)) {
		t.Errorf("expected full label with --full, got: %s", fullOut)
	}
	if !strings.Contains(fullOut, fmt.Sprintf(`value=%q`, longValue)) {
		t.Errorf("expected full value with --full, got: %s", fullOut)
	}
}

func TestFormStateMaxControlsCapAndFull(t *testing.T) {
	_, token := setupStatusEnv(t)
	var controls []string
	for i := 1; i <= 205; i++ {
		controls = append(controls, fmt.Sprintf(`{"frame":"top","tag":"input","type":"text","name":"field_%03d"}`, i))
	}
	mockResponse := `{"controls":[` + strings.Join(controls, ",") + `]}`
	srv := fakePhase2Server(t, token, phase2Result(mockResponse), nil)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	defaultOut, err := executePhase2(t, "formstate")
	if err != nil {
		t.Fatalf("formstate default error = %v", err)
	}
	if !strings.Contains(defaultOut, `name="field_200"`) || strings.Contains(defaultOut, `name="field_201"`) {
		t.Errorf("expected default cap at 200 controls, got: %s", defaultOut)
	}
	if !strings.Contains(defaultOut, "[truncated: showing 200 of 205 controls; use --full or --frame]") {
		t.Errorf("expected default truncation notice, got: %s", defaultOut)
	}

	customOut, err := executePhase2(t, "formstate", "--max-controls", "3")
	if err != nil {
		t.Fatalf("formstate --max-controls 3 error = %v", err)
	}
	if !strings.Contains(customOut, `name="field_003"`) || strings.Contains(customOut, `name="field_004"`) {
		t.Errorf("expected cap at 3 controls, got: %s", customOut)
	}
	if !strings.Contains(customOut, "[truncated: showing 3 of 205 controls; use --full or --frame]") {
		t.Errorf("expected custom truncation notice, got: %s", customOut)
	}

	fullOut, err := executePhase2(t, "formstate", "--max-controls", "3", "--full")
	if err != nil {
		t.Fatalf("formstate --full error = %v", err)
	}
	if !strings.Contains(fullOut, `name="field_205"`) {
		t.Errorf("expected all 205 controls with --full, got: %s", fullOut)
	}
	if strings.Contains(fullOut, "[truncated:") {
		t.Errorf("expected no truncation notice with --full, got: %s", fullOut)
	}

	for _, bad := range []string{"0", "-1"} {
		if _, err := executePhase2(t, "formstate", "--max-controls", bad); err == nil {
			t.Errorf("formstate --max-controls %s error = nil, want error", bad)
		}
	}
}
