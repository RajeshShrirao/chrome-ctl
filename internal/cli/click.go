package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const clickTimeout = 70 * time.Second

func newClickCmd() *cobra.Command {
	var snap, double bool
	cmd := &cobra.Command{
		Use:   "click <id>",
		Short: "Click an indexed element",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseActionID(args[0])
			if err != nil {
				return err
			}

			params := protocol.ActionClickParams{ID: id, DoubleClick: double, Snap: snap}
			var result protocol.ActionClickResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionClick, params, &result, clickTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatClick(result, double))
			return nil
		},
	}
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a page snapshot after clicking")
	cmd.Flags().BoolVar(&double, "double", false, "Double-click instead of a single click")
	return cmd
}

// parseActionID accepts a positive element id.
func parseActionID(arg string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid element id %q: must be a positive integer", arg)
	}
	return id, nil
}

func formatClick(result protocol.ActionClickResult, double bool) string {
	action := "clicked"
	if double {
		action = "double-clicked"
	}

	line := fmt.Sprintf("OK: %s [%d] <%s> %q @%d,%d (%.2fs)",
		action,
		result.Target.ID,
		result.Target.Tag,
		result.Target.Label,
		result.Coordinates.X,
		result.Coordinates.Y,
		float64(result.DurationMs)/1000,
	)
	if result.Snapshot != nil {
		return line + "\n\n" + ensureTrailingNewline(result.Snapshot.FormattedText)
	}
	return line + "\n"
}
