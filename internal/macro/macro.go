// Package macro implements the daemon-independent playbook logic: template
// interpolation, parameterization of recorded steps, and on-disk storage of
// macro files under ~/.chrome-ctl/macros.
package macro

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const (
	// Schema is the JSON schema identifier written to every saved macro.
	Schema = "https://chrome-ctl.dev/schemas/macro-v1.json"
	// Version is the macro document version written to every saved macro.
	Version = "1.0"
)

var (
	namePattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	templatePattern = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
)

// ValidName reports whether name is safe to use as a macro file name. It
// rejects empty names, path separators, and the "." / ".." traversal names.
func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return namePattern.MatchString(name)
}

// Interpolate expands the supported template expressions in template:
//
//	{{name}}
//	{{trim name}}
//	{{lowercase name}}
//	{{default name "fallback"}}
//
// Whitespace inside the braces is flexible. A missing variable is an error
// unless a default is supplied.
func Interpolate(template string, vars map[string]string) (string, error) {
	var evalErr error
	out := templatePattern.ReplaceAllStringFunc(template, func(match string) string {
		if evalErr != nil {
			return match
		}
		value, err := evalExpression(match[2:len(match)-2], vars)
		if err != nil {
			evalErr = err
			return match
		}
		return value
	})
	if evalErr != nil {
		return "", evalErr
	}
	return out, nil
}

func evalExpression(expr string, vars map[string]string) (string, error) {
	inner := strings.TrimSpace(expr)
	if inner == "" {
		return "", fmt.Errorf("unsupported expression %q", "{{}}")
	}

	fields := strings.Fields(inner)
	if len(fields) == 1 && fields[0] != "default" {
		value, ok := vars[fields[0]]
		if !ok {
			return "", fmt.Errorf("unknown variable %q", fields[0])
		}
		return value, nil
	}

	if fields[0] == "default" {
		rest := strings.TrimSpace(inner[len("default"):])
		sep := strings.IndexAny(rest, " \t")
		if sep < 0 {
			return "", fmt.Errorf("unsupported expression %q", inner)
		}
		name := rest[:sep]
		fallback, err := strconv.Unquote(strings.TrimSpace(rest[sep:]))
		if err != nil {
			return "", fmt.Errorf("unsupported expression %q", inner)
		}
		if vars[name] == "" {
			return fallback, nil
		}
		return vars[name], nil
	}

	if len(fields) == 2 {
		name := fields[1]
		value, ok := vars[name]
		if !ok {
			return "", fmt.Errorf("unknown variable %q", name)
		}
		switch fields[0] {
		case "trim":
			return strings.TrimSpace(value), nil
		case "lowercase":
			return strings.ToLower(value), nil
		}
	}

	return "", fmt.Errorf("unsupported expression %q", inner)
}

// Parameterize binds params sequentially to the value-bearing steps of file
// (open -> url, type -> value, upload -> value), replacing the recorded
// literal with "{{name}}". Extra parameters are rejected.
func Parameterize(file *protocol.MacroFile, params []string) error {
	stepFields := make([]string, 0, len(file.Steps))
	for _, step := range file.Steps {
		if field := valueField(step.Action); field != "" {
			stepFields = append(stepFields, field)
		}
	}
	if len(params) > len(stepFields) {
		return fmt.Errorf("parameter %q has no value-bearing step to bind", params[len(stepFields)])
	}

	next := 0
	for i := range file.Steps {
		field := valueField(file.Steps[i].Action)
		if field == "" || next >= len(params) {
			continue
		}
		placeholder := "{{" + params[next] + "}}"
		switch field {
		case "url":
			file.Steps[i].URL = placeholder
		case "value":
			file.Steps[i].Value = placeholder
		}
		next++
	}

	file.Parameters = make([]protocol.MacroParameter, 0, len(params))
	for _, name := range params {
		file.Parameters = append(file.Parameters, protocol.MacroParameter{
			Name:     name,
			Type:     "string",
			Required: true,
		})
	}
	return nil
}

func valueField(action string) string {
	switch action {
	case "open":
		return "url"
	case "type", "upload":
		return "value"
	default:
		return ""
	}
}

// Save writes file to dir/<name>.json atomically and returns the absolute path.
func Save(dir string, file protocol.MacroFile) (string, error) {
	if !ValidName(file.Name) {
		return "", fmt.Errorf("invalid macro name %q", file.Name)
	}
	if dir == "" {
		return "", errors.New("macro directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create macro directory: %w", err)
	}

	file.Schema = Schema
	file.MacroVersion = Version
	if file.CreatedAt == "" {
		file.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode macro: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(dir, file.Name+".json")
	tmp, err := os.CreateTemp(dir, ".macro-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create macro temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("set macro file mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", fmt.Errorf("write macro: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", fmt.Errorf("close macro: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return "", fmt.Errorf("publish macro: %w", err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

// Load reads dir/<name>.json.
func Load(dir, name string) (protocol.MacroFile, error) {
	var file protocol.MacroFile
	if !ValidName(name) {
		return file, fmt.Errorf("invalid macro name %q", name)
	}
	data, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return file, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return protocol.MacroFile{}, fmt.Errorf("decode macro %q: %w", name, err)
	}
	return file, nil
}

// List returns summaries for every readable macro in dir, sorted by name. A
// missing directory yields an empty, non-nil slice.
func List(dir string) ([]protocol.MacroSummary, error) {
	summaries := make([]protocol.MacroSummary, 0)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return summaries, nil
		}
		return nil, fmt.Errorf("read macro directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if name == entry.Name() || !ValidName(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var file protocol.MacroFile
		if err := json.Unmarshal(data, &file); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}

		params := make([]string, 0, len(file.Parameters))
		for _, param := range file.Parameters {
			params = append(params, param.Name)
		}
		summaries = append(summaries, protocol.MacroSummary{
			Name:        name,
			Description: file.Description,
			Parameters:  params,
			Steps:       len(file.Steps),
			ModifiedAt:  info.ModTime().UTC().Format(time.RFC3339),
		})
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Name < summaries[j].Name })
	return summaries, nil
}
