package macro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestInterpolate(t *testing.T) {
	vars := map[string]string{
		"name":  "  Apex Labs ",
		"email": "Team@Apex.IO",
		"empty": "",
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{name: "direct", template: "{{name}}", want: "  Apex Labs "},
		{name: "trim", template: "{{trim name}}", want: "Apex Labs"},
		{name: "trim flexible whitespace", template: "{{  trim   name  }}", want: "Apex Labs"},
		{name: "lowercase", template: "{{lowercase email}}", want: "team@apex.io"},
		{name: "default used when empty", template: `{{default empty "fallback"}}`, want: "fallback"},
		{name: "default skipped when present", template: `{{default email "fallback"}}`, want: "Team@Apex.IO"},
		{name: "default with spaces", template: `{{ default missing "/tmp/my file.png" }}`, want: "/tmp/my file.png"},
		{name: "surrounding text", template: "Hi {{trim name}} <{{lowercase email}}>", want: "Hi Apex Labs <team@apex.io>"},
		{name: "literal", template: "no expressions here", want: "no expressions here"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Interpolate(tc.template, vars)
			if err != nil {
				t.Fatalf("Interpolate(%q) error: %v", tc.template, err)
			}
			if got != tc.want {
				t.Fatalf("Interpolate(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}

	errorTests := []struct {
		name     string
		template string
		wantErr  string
	}{
		{name: "missing variable", template: "{{missing}}", wantErr: `unknown variable "missing"`},
		{name: "missing transform variable", template: "{{trim missing}}", wantErr: `unknown variable "missing"`},
		{name: "unknown transform", template: "{{uppercase name}}", wantErr: "unsupported expression"},
		{name: "empty expression", template: "{{}}", wantErr: "unsupported expression"},
		{name: "default without fallback", template: "{{default name}}", wantErr: "unsupported expression"},
		{name: "default unquoted fallback", template: "{{default name fallback}}", wantErr: "unsupported expression"},
	}
	for _, tc := range errorTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Interpolate(tc.template, vars)
			if err == nil {
				t.Fatalf("Interpolate(%q) = nil error, want %q", tc.template, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestParameterizeOrder(t *testing.T) {
	file := protocol.MacroFile{
		Name: "bind",
		Steps: []protocol.MacroStep{
			{StepID: 1, Action: "open", URL: "https://recorded.example.com"},
			{StepID: 2, Action: "click", Target: &protocol.MacroTarget{AriaLabel: "Go"}},
			{StepID: 3, Action: "type", Value: "recorded text"},
			{StepID: 4, Action: "upload", Value: "/tmp/recorded.png"},
		},
	}

	if err := Parameterize(&file, []string{"target_url", "message", "file_path"}); err != nil {
		t.Fatalf("Parameterize: %v", err)
	}
	if file.Steps[0].URL != "{{target_url}}" {
		t.Fatalf("open url = %q, want {{target_url}}", file.Steps[0].URL)
	}
	if file.Steps[1].Value != "" {
		t.Fatalf("click value = %q, want untouched", file.Steps[1].Value)
	}
	if file.Steps[2].Value != "{{message}}" {
		t.Fatalf("type value = %q, want {{message}}", file.Steps[2].Value)
	}
	if file.Steps[3].Value != "{{file_path}}" {
		t.Fatalf("upload value = %q, want {{file_path}}", file.Steps[3].Value)
	}
	if len(file.Parameters) != 3 {
		t.Fatalf("parameters = %+v, want 3", file.Parameters)
	}
	for i, name := range []string{"target_url", "message", "file_path"} {
		param := file.Parameters[i]
		if param.Name != name || param.Type != "string" || !param.Required {
			t.Fatalf("parameter %d = %+v, want name=%q type=string required", i, param, name)
		}
	}

	extra := protocol.MacroFile{
		Name: "extra",
		Steps: []protocol.MacroStep{
			{StepID: 1, Action: "open", URL: "https://example.com"},
		},
	}
	err := Parameterize(&extra, []string{"one", "surplus"})
	if err == nil || !strings.Contains(err.Error(), "surplus") {
		t.Fatalf("extra params error = %v, want mention of surplus", err)
	}
}

func TestParameterizeNoParamsKeepsLiterals(t *testing.T) {
	file := protocol.MacroFile{
		Name: "literal",
		Steps: []protocol.MacroStep{
			{StepID: 1, Action: "open", URL: "https://example.com"},
			{StepID: 2, Action: "type", Value: "unchanged"},
		},
	}
	if err := Parameterize(&file, nil); err != nil {
		t.Fatalf("Parameterize: %v", err)
	}
	if file.Steps[0].URL != "https://example.com" || file.Steps[1].Value != "unchanged" {
		t.Fatalf("steps were rewritten: %+v", file.Steps)
	}
	if file.Parameters == nil || len(file.Parameters) != 0 {
		t.Fatalf("parameters = %#v, want non-nil empty slice", file.Parameters)
	}
}

func TestSaveLoadListRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "macros")
	file := protocol.MacroFile{
		Name:        "register",
		Description: "Registers a listing",
		Steps: []protocol.MacroStep{
			{StepID: 1, Action: "open", URL: "{{url}}", Description: "Navigate"},
			{StepID: 2, Action: "type", Target: &protocol.MacroTarget{AriaLabel: "Company"}, Value: "{{company}}"},
		},
		Parameters: []protocol.MacroParameter{{Name: "url", Type: "string", Required: true}, {Name: "company", Type: "string", Required: true}},
	}

	path, err := Save(dir, file)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("path = %q, want absolute", path)
	}
	if want := filepath.Join(dir, "register.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat macro dir: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("dir mode = %o, want 700", mode)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat macro file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o, want 600", mode)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read macro: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode macro json: %v", err)
	}
	if doc["$schema"] != Schema {
		t.Fatalf("$schema = %v, want %q", doc["$schema"], Schema)
	}
	if doc["macro_version"] != Version {
		t.Fatalf("macro_version = %v, want %q", doc["macro_version"], Version)
	}
	created, ok := doc["created_at"].(string)
	if !ok {
		t.Fatalf("created_at = %v, want string", doc["created_at"])
	}
	if _, err := time.Parse(time.RFC3339, created); err != nil {
		t.Fatalf("created_at = %q, not RFC3339: %v", created, err)
	}

	loaded, err := Load(dir, "register")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Schema != Schema || loaded.MacroVersion != Version || loaded.Name != "register" {
		t.Fatalf("loaded = %+v, want schema/version/name set", loaded)
	}
	if len(loaded.Steps) != 2 || len(loaded.Parameters) != 2 {
		t.Fatalf("loaded steps/parameters = %d/%d, want 2/2", len(loaded.Steps), len(loaded.Parameters))
	}
	if loaded.Steps[1].Target == nil || loaded.Steps[1].Target.AriaLabel != "Company" {
		t.Fatalf("loaded target = %+v, want Company", loaded.Steps[1].Target)
	}

	if _, err := Save(dir, protocol.MacroFile{Name: "alpha", Steps: []protocol.MacroStep{{StepID: 1, Action: "click"}}}); err != nil {
		t.Fatalf("Save alpha: %v", err)
	}
	summaries, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("list = %+v, want 2 entries", summaries)
	}
	if summaries[0].Name != "alpha" || summaries[1].Name != "register" {
		t.Fatalf("list order = %v, want name-sorted", []string{summaries[0].Name, summaries[1].Name})
	}
	got := summaries[1]
	if got.Steps != 2 {
		t.Fatalf("steps = %d, want 2", got.Steps)
	}
	if len(got.Parameters) != 2 || got.Parameters[0] != "url" || got.Parameters[1] != "company" {
		t.Fatalf("parameters = %v, want [url company]", got.Parameters)
	}
	if _, err := time.Parse(time.RFC3339, got.ModifiedAt); err != nil {
		t.Fatalf("modifiedAt = %q, not RFC3339: %v", got.ModifiedAt, err)
	}
}

func TestListMissingDirectory(t *testing.T) {
	summaries, err := List(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if summaries == nil || len(summaries) != 0 {
		t.Fatalf("list = %#v, want non-nil empty slice", summaries)
	}
}

func TestNameValidation(t *testing.T) {
	dir := t.TempDir()
	bad := []string{"", ".", "..", "../escape", "a/b", `a\b`, "a b", "a$", "a+", strings.Repeat("a", 1) + "/x"}
	for _, name := range bad {
		if ValidName(name) {
			t.Fatalf("ValidName(%q) = true, want false", name)
		}
		if _, err := Save(dir, protocol.MacroFile{Name: name}); err == nil {
			t.Fatalf("Save(%q) = nil error, want rejection", name)
		}
		if _, err := Load(dir, name); err == nil {
			t.Fatalf("Load(%q) = nil error, want rejection", name)
		}
	}
	for _, name := range []string{"a", "a.b", "a_b", "a-b", "Macro.1"} {
		if !ValidName(name) {
			t.Fatalf("ValidName(%q) = false, want true", name)
		}
	}
}
