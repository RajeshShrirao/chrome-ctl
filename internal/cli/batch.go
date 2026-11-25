package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newBatchCmd() *cobra.Command {
	var snapOnError bool
	cmd := &cobra.Command{
		Use:   "batch <command> [command...]",
		Short: "Run an inline sequence of commands stopping on first failure with fresh snap",
		Long: `Run an inline sequence of chrome-ctl subcommands (e.g. 'click 5', 'wait --text Saved').
Verifies each step before executing the next.
If a step fails, stops immediately, never auto-retries, and attaches a fresh snapshot.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBatch(cmd, args, snapOnError)
		},
	}
	cmd.Flags().BoolVar(&snapOnError, "snap-on-error", true, "Attach a fresh page snap on failure")
	return cmd
}

func runBatch(cmd *cobra.Command, steps []string, snapOnError bool) error {
	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	for i, step := range steps {
		stepNum := i + 1
		tokens, err := splitBatchArgs(step)
		if err != nil {
			return fmt.Errorf("batch step %d: %w", stepNum, err)
		}
		if len(tokens) == 0 {
			continue
		}

		// ExecuteContext always runs from the root command and falls back to
		// os.Args[1:] when root.args is nil, so configure and execute root.
		root, err := findSubCommand(tokens)
		if err != nil {
			return fmt.Errorf("batch step %d: %w", stepNum, err)
		}

		var stepOut bytes.Buffer
		var stepErr bytes.Buffer
		root.SetOut(&stepOut)
		root.SetErr(&stepErr)
		root.SetArgs(tokens)

		err = root.ExecuteContext(cmd.Context())
		if err != nil {
			failMsg := fmt.Sprintf("BATCH STEP FAILED: step %d (%q): %v\n", stepNum, step, err)
			if stepErr.Len() > 0 {
				failMsg += strings.TrimSpace(stepErr.String()) + "\n"
			}

			if snapOnError {
				freshSnap := takeFreshSnapshot(cmd.Context())
				if freshSnap != "" {
					failMsg += "\n" + ensureTrailingNewline(freshSnap)
				}
			}

			fmt.Fprint(stderr, failMsg)
			return &ExitError{Code: 1, Silent: true}
		}

		// Print output of successful step if any
		if stepOut.Len() > 0 {
			fmt.Fprintf(stdout, "[%d/%d] %s", stepNum, len(steps), stepOut.String())
		}
	}

	return nil
}

// findSubCommand resolves tokens against a fresh root command, rejects unknown
// commands and nested batch invocations, and returns that root ready to execute.
func findSubCommand(tokens []string) (*cobra.Command, error) {
	root := NewRoot()
	target, _, err := root.Find(tokens)
	if err != nil {
		return nil, err
	}
	if target == nil || target == root {
		return nil, fmt.Errorf("unknown command %q", tokens[0])
	}
	for c := target; c != nil; c = c.Parent() {
		if c.Name() == "batch" {
			return nil, errors.New("nested batch command is not allowed")
		}
	}
	return root, nil
}

// splitBatchArgs splits a batch step string into CLI tokens, supporting single
// quotes ('...'), double quotes ("..."), and backslash escapes.
func splitBatchArgs(input string) ([]string, error) {
	var args []string
	var buf strings.Builder
	inToken := false
	var quote rune
	escaped := false

	for _, r := range input {
		if escaped {
			if quote == '"' && r != '"' && r != '\\' {
				buf.WriteByte('\\')
			}
			buf.WriteRune(r)
			escaped = false
			inToken = true
			continue
		}

		switch quote {
		case '\'':
			if r == '\'' {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
		case '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				buf.WriteRune(r)
			}
		default:
			switch {
			case unicode.IsSpace(r):
				if inToken {
					args = append(args, buf.String())
					buf.Reset()
					inToken = false
				}
			case r == '\'', r == '"':
				quote = r
				inToken = true
			case r == '\\':
				escaped = true
				inToken = true
			default:
				buf.WriteRune(r)
				inToken = true
			}
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c quote", quote)
	}
	if escaped {
		return nil, errors.New("unterminated backslash escape")
	}
	if inToken {
		args = append(args, buf.String())
	}
	return args, nil
}

func takeFreshSnapshot(ctx context.Context) string {
	c, err := newRPCClient()
	if err != nil {
		return ""
	}
	params := protocol.PageSnapParams{}
	raw, rpcErr, err := c.Call(ctx, protocol.MethodPageSnap, params)
	if err != nil || rpcErr != nil {
		return ""
	}
	var res protocol.SnapResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return ""
	}
	return res.FormattedText
}
