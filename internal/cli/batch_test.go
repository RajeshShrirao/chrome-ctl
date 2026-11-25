package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/buildinfo"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestBatchCommandValidation(t *testing.T) {
	root := NewRoot()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"batch"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when batch is called with no arguments")
	}
}

func TestBatchCommandUnknownSubcommand(t *testing.T) {
	root := NewRoot()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{"batch", "nonexistent-action-xyz 123"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command error, got: %v", err)
	}
}

func TestSplitBatchArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr string
	}{
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   \t\n  ",
			want:  nil,
		},
		{
			name:  "simple tokens",
			input: "click 5",
			want:  []string{"click", "5"},
		},
		{
			name:  "single quoted argument with spaces",
			input: "type 3 'Alice Smith' --enter",
			want:  []string{"type", "3", "Alice Smith", "--enter"},
		},
		{
			name:  "double quoted argument with spaces",
			input: `type 3 "Alice Smith" --enter`,
			want:  []string{"type", "3", "Alice Smith", "--enter"},
		},
		{
			name:  "empty single and double quoted arguments",
			input: `type 3 '' "" --enter`,
			want:  []string{"type", "3", "", "", "--enter"},
		},
		{
			name:  "quotes embedded inside opposite quotes",
			input: `type 3 "O'Brien" 'Say "hi"'`,
			want:  []string{"type", "3", "O'Brien", `Say "hi"`},
		},
		{
			name:  "adjacent quoted segments in one token",
			input: `wait --for-text='Saved OK' --flag="a b"`,
			want:  []string{"wait", "--for-text=Saved OK", "--flag=a b"},
		},
		{
			name:  "backslash escapes outside quotes",
			input: `type 3 Alice\ Smith a\\b \'hi\'`,
			want:  []string{"type", "3", "Alice Smith", `a\b`, "'hi'"},
		},
		{
			name:  "backslash escapes inside double quotes",
			input: `type 3 "Say \"hi\" C:\\dir\n"`,
			want:  []string{"type", "3", `Say "hi" C:\dir\n`},
		},
		{
			name:  "backslashes preserved literally inside single quotes",
			input: `eval '/\d+/'`,
			want:  []string{"eval", `/\d+/`},
		},
		{
			name:  "utf8 runes inside quotes",
			input: `type 3 'Café Résumé' --enter`,
			want:  []string{"type", "3", "Café Résumé", "--enter"},
		},
		{
			name:    "unclosed single quote",
			input:   "type 3 'Alice",
			wantErr: "unclosed",
		},
		{
			name:    "unclosed double quote",
			input:   `type 3 "Alice`,
			wantErr: "unclosed",
		},
		{
			name:    "unclosed double quote ending with backslash",
			input:   `type 3 "Alice\`,
			wantErr: "unclosed",
		},
		{
			name:    "trailing backslash outside quotes",
			input:   `type 3 Alice\`,
			wantErr: "unterminated backslash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := splitBatchArgs(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("splitBatchArgs(%q) error = nil, want %q", tt.input, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("splitBatchArgs(%q) error = %q, want substring %q", tt.input, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitBatchArgs(%q) unexpected error = %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("splitBatchArgs(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestBatchNoRecursionWithOSArgs(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"chrome-ctl", "batch", "version"}
	t.Cleanup(func() { os.Args = oldArgs })

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	os.Stdout = wOut
	os.Stderr = wErr
	t.Cleanup(func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	})

	code := Execute()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	stdoutBytes, _ := io.ReadAll(rOut)
	stderrBytes, _ := io.ReadAll(rErr)
	_ = rOut.Close()
	_ = rErr.Close()

	if code != 0 {
		t.Fatalf("Execute() = %d, want 0; stderr = %q", code, stderrBytes)
	}
	wantOut := "[1/1] chrome-ctl " + buildinfo.Version + "\n"
	if string(stdoutBytes) != wantOut {
		t.Errorf("stdout = %q, want %q", string(stdoutBytes), wantOut)
	}
	if len(stderrBytes) != 0 {
		t.Errorf("stderr = %q, want empty", string(stderrBytes))
	}
}

func TestBatchRejectsNestedBatch(t *testing.T) {
	tests := []struct {
		name  string
		steps []string
	}{
		{name: "nested batch version", steps: []string{"batch version"}},
		{name: "nested bare batch", steps: []string{"batch"}},
		{name: "nested batch on second step", steps: []string{"version", "batch version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := NewRoot()
			var outBuf, errBuf bytes.Buffer
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs(append([]string{"batch"}, tt.steps...))

			err := root.Execute()
			if err == nil {
				t.Fatal("expected nested batch error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "nested batch") {
				t.Fatalf("error = %q, want nested batch error", err.Error())
			}
		})
	}
}

func newBatchRPCServer(t *testing.T, token string, handler func(req phase2Request) string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q, want %q", got, "Bearer "+token)
		}
		var req phase2Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, handler(req))
	}))
}

func TestBatchMultiStepQuotedArguments(t *testing.T) {
	_, token := setupStatusEnv(t)
	var requests []phase2Request

	srv := newBatchRPCServer(t, token, func(req phase2Request) string {
		requests = append(requests, req)
		switch req.Method {
		case protocol.MethodActionClick:
			return phase2Result(`{"success":true,"action":"click","durationMs":120}`)
		case protocol.MethodActionType:
			var params protocol.ActionTypeParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Errorf("decode type params: %v", err)
			}
			return phase2Result(
				`{"success":true,"charsTyped":` + strconv.Itoa(len(params.Text)) + `,"durationMs":300,"verified":true}`)
		case protocol.MethodPageRead:
			return phase2Result(`{"markdown":"# Saved\n"}`)
		default:
			t.Errorf("unexpected RPC method %q", req.Method)
			return phase2Error(-32601, "method not found")
		}
	})
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	root := NewRoot()
	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"batch",
		"click 5",
		"type 3 'Alice Smith' --enter",
		`type 4 "Bob \"B\" Jones"`,
		"read",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("batch unexpected error: %v (stderr: %q)", err, errBuf.String())
	}
	if errBuf.Len() != 0 {
		t.Errorf("stderr = %q, want empty", errBuf.String())
	}

	if len(requests) != 4 {
		t.Fatalf("got %d RPC requests, want 4", len(requests))
	}

	var clickParams protocol.ActionClickParams
	if err := json.Unmarshal(requests[0].Params, &clickParams); err != nil {
		t.Fatalf("decode click params: %v", err)
	}
	if requests[0].Method != protocol.MethodActionClick || clickParams.ID != 5 {
		t.Errorf("step 1 = (%s, %+v), want click ID 5", requests[0].Method, clickParams)
	}

	var type1 protocol.ActionTypeParams
	if err := json.Unmarshal(requests[1].Params, &type1); err != nil {
		t.Fatalf("decode type1 params: %v", err)
	}
	if requests[1].Method != protocol.MethodActionType || type1.ID != 3 || type1.Text != "Alice Smith" || !type1.Enter {
		t.Errorf("step 2 = (%s, %+v), want id=3 text=%q enter=true", requests[1].Method, type1, "Alice Smith")
	}

	var type2 protocol.ActionTypeParams
	if err := json.Unmarshal(requests[2].Params, &type2); err != nil {
		t.Fatalf("decode type2 params: %v", err)
	}
	if requests[2].Method != protocol.MethodActionType || type2.ID != 4 || type2.Text != `Bob "B" Jones` || type2.Enter {
		t.Errorf("step 3 = (%s, %+v), want id=4 text=%q enter=false", requests[2].Method, type2, `Bob "B" Jones`)
	}

	if requests[3].Method != protocol.MethodPageRead {
		t.Errorf("step 4 method = %q, want %q", requests[3].Method, protocol.MethodPageRead)
	}

	out := outBuf.String()
	for _, wantLine := range []string{
		"[1/4] OK: clicked (0.12s)",
		"[2/4] OK: Typed 11 chars into [3] (verified; human cadence: 0.3s).",
		"[3/4] OK: Typed 13 chars into [4] (verified; human cadence: 0.3s).",
		"[4/4] # Saved",
	} {
		if !strings.Contains(out, wantLine) {
			t.Errorf("stdout missing %q;\ngot:\n%s", wantLine, out)
		}
	}
}

func TestBatchFailFastAndSnapOnError(t *testing.T) {
	t.Run("snap on error enabled by default", func(t *testing.T) {
		_, token := setupStatusEnv(t)
		var methods []string

		srv := newBatchRPCServer(t, token, func(req phase2Request) string {
			methods = append(methods, req.Method)
			switch req.Method {
			case protocol.MethodActionClick:
				var params protocol.ActionClickParams
				if err := json.Unmarshal(req.Params, &params); err != nil {
					t.Errorf("decode click params: %v", err)
				}
				if params.ID == 5 {
					return phase2Result(`{"success":true,"action":"click","durationMs":100}`)
				}
				return phase2Error(protocol.ErrElementNotFound, "element [99] not found")
			case protocol.MethodPageSnap:
				return phase2Result(`{"formattedText":"PAGE [1] Example\n[1] <button> Retry\n"}`)
			default:
				t.Errorf("unexpected RPC method %q (step 3 should not execute)", req.Method)
				return phase2Error(-32601, "method not found")
			}
		})
		defer srv.Close()
		t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

		root := NewRoot()
		var outBuf, errBuf bytes.Buffer
		root.SetOut(&outBuf)
		root.SetErr(&errBuf)
		root.SetArgs([]string{
			"batch",
			"click 5",
			"click 99",
			"type 3 'should never execute'",
		})

		err := root.Execute()
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 1 || !exitErr.Silent {
			t.Fatalf("err = %v, want silent ExitError(1)", err)
		}

		wantMethods := []string{
			protocol.MethodActionClick,
			protocol.MethodActionClick,
			protocol.MethodPageSnap,
		}
		if !reflect.DeepEqual(methods, wantMethods) {
			t.Errorf("RPC methods = %v, want %v", methods, wantMethods)
		}

		if gotOut := outBuf.String(); !strings.Contains(gotOut, "[1/3] OK: clicked") || strings.Contains(gotOut, "[3/3]") {
			t.Errorf("stdout = %q, want only step 1 output", gotOut)
		}

		wantErr := "BATCH STEP FAILED: step 2 (\"click 99\"): element [99] not found\n\nPAGE [1] Example\n[1] <button> Retry\n"
		if errBuf.String() != wantErr {
			t.Errorf("stderr =\n%q\nwant\n%q", errBuf.String(), wantErr)
		}
	})

	t.Run("snap on error disabled", func(t *testing.T) {
		_, token := setupStatusEnv(t)
		var methods []string

		srv := newBatchRPCServer(t, token, func(req phase2Request) string {
			methods = append(methods, req.Method)
			return phase2Error(protocol.ErrElementNotFound, "element [99] not found")
		})
		defer srv.Close()
		t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

		root := NewRoot()
		var outBuf, errBuf bytes.Buffer
		root.SetOut(&outBuf)
		root.SetErr(&errBuf)
		root.SetArgs([]string{
			"batch",
			"--snap-on-error=false",
			"click 99",
			"type 3 'should never execute'",
		})

		err := root.Execute()
		var exitErr *ExitError
		if !errors.As(err, &exitErr) || exitErr.Code != 1 || !exitErr.Silent {
			t.Fatalf("err = %v, want silent ExitError(1)", err)
		}

		wantMethods := []string{protocol.MethodActionClick}
		if !reflect.DeepEqual(methods, wantMethods) {
			t.Errorf("RPC methods = %v, want %v", methods, wantMethods)
		}

		wantErr := "BATCH STEP FAILED: step 1 (\"click 99\"): element [99] not found\n"
		if errBuf.String() != wantErr {
			t.Errorf("stderr =\n%q\nwant\n%q", errBuf.String(), wantErr)
		}
	})
}
