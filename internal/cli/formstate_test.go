package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
