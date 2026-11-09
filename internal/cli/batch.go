package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
		fields := strings.Fields(strings.TrimSpace(step))
		if len(fields) == 0 {
			continue
		}

		// Rebuild subcommand execution within root context
		subCmd, subArgs, err := findSubCommand(fields)
		if err != nil {
			return fmt.Errorf("batch step %d: %w", stepNum, err)
		}

		var stepOut bytes.Buffer
		var stepErr bytes.Buffer
		subCmd.SetOut(&stepOut)
		subCmd.SetErr(&stepErr)
		subCmd.SetArgs(subArgs)

		err = subCmd.ExecuteContext(cmd.Context())
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

func findSubCommand(tokens []string) (*cobra.Command, []string, error) {
	root := NewRoot()
	target, args, err := root.Find(tokens)
	if err != nil {
		return nil, nil, err
	}
	if target == nil || target == root {
		return nil, nil, fmt.Errorf("unknown command %q", tokens[0])
	}
	return target, args, nil
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
