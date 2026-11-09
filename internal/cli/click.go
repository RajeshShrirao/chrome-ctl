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
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "click <id>",
		Short: "Click an indexed element",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseActionID(args[0])
			if err != nil {
				return err
			}

			params := protocol.ActionClickParams{
				ID:          id,
				DoubleClick: double,
				Snap:        snap,
				TimeoutMs:   int(timeout.Milliseconds()),
			}
			var result protocol.ActionClickResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionClick, params, &result, timeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatClick(result, double))
			return nil
		},
	}
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a page snapshot after clicking")
	cmd.Flags().BoolVar(&double, "double", false, "Double-click instead of a single click")
	cmd.Flags().DurationVar(&timeout, "timeout", clickTimeout, "Timeout for the click operation")
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

	timing := fmt.Sprintf("%.2fs", float64(result.DurationMs)/1000)
	if result.DispatchMs > 0 || result.SettleMs > 0 {
		timing = fmt.Sprintf("%.2fs: dispatch %.2fs, DOM settle %.2fs",
			float64(result.DurationMs)/1000,
			float64(result.DispatchMs)/1000,
			float64(result.SettleMs)/1000,
		)
	}

	var line string
	switch {
	case result.Target != nil:
		coords := protocol.ActionCoordinates{}
		if result.Coordinates != nil {
			coords = *result.Coordinates
		}
		line = fmt.Sprintf("OK: %s [%d] <%s> %q @%d,%d (%s)",
			action,
			result.Target.ID,
			result.Target.Tag,
			result.Target.Label,
			coords.X,
			coords.Y,
			timing,
		)
	case result.Coordinates != nil:
		line = fmt.Sprintf("OK: %s @%d,%d (%s)",
			action,
			result.Coordinates.X,
			result.Coordinates.Y,
			timing,
		)
	default:
		line = fmt.Sprintf("OK: %s (%s)", action, timing)
	}
	if result.LandedOn != "" {
		line += fmt.Sprintf(" Landed on %s.", result.LandedOn)
	}
	if result.URLChanged {
		line += " URL changed."
	}
	if result.Snapshot != nil {
		return line + "\n\n" + ensureTrailingNewline(result.Snapshot.FormattedText)
	}
	return line + "\n"
}
