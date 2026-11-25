package cli

import (
	"errors"
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
	var seed string
	cmd := &cobra.Command{
		Use:   "click <id | text | css=selector>",
		Short: "Click an element by id, visible text, or CSS selector",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := strings.TrimSpace(args[0])
			params := protocol.ActionClickParams{
				DoubleClick: double,
				Snap:        snap,
				TimeoutMs:   int(timeout.Milliseconds()),
				Seed:        seed,
			}

			if id, err := strconv.Atoi(arg); err == nil {
				if id <= 0 {
					return fmt.Errorf("invalid element id %q: must be a positive integer", arg)
				}
				params.ID = id
			} else if selector, ok := strings.CutPrefix(arg, "css="); ok {
				if strings.TrimSpace(selector) == "" {
					return errors.New("css selector cannot be empty")
				}
				params.Selector = selector
			} else {
				if arg == "" {
					return errors.New("target cannot be empty")
				}
				params.Text = arg
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
	cmd.Flags().StringVar(&seed, "seed", "", "Deterministic seed for humanized cursor trajectory and landing point")
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

	consequence := ""
	if result.Consequence != "" {
		consequence = " -> " + result.Consequence
	}

	var line string
	switch {
	case result.Target != nil:
		coords := protocol.ActionCoordinates{}
		if result.Coordinates != nil {
			coords = *result.Coordinates
		}
		if result.Target.ID > 0 {
			line = fmt.Sprintf("OK: %s [%d] <%s> %q @%d,%d%s (%s)",
				action,
				result.Target.ID,
				result.Target.Tag,
				result.Target.Label,
				coords.X,
				coords.Y,
				consequence,
				timing,
			)
		} else {
			line = fmt.Sprintf("OK: %s <%s> %q @%d,%d%s (%s)",
				action,
				result.Target.Tag,
				result.Target.Label,
				coords.X,
				coords.Y,
				consequence,
				timing,
			)
		}
	case result.Coordinates != nil:
		line = fmt.Sprintf("OK: %s @%d,%d%s (%s)",
			action,
			result.Coordinates.X,
			result.Coordinates.Y,
			consequence,
			timing,
		)
	default:
		line = fmt.Sprintf("OK: %s%s (%s)", action, consequence, timing)
	}
	if result.LandedOn != "" {
		line += fmt.Sprintf(" Landed on %s.", result.LandedOn)
	}
	if result.URLChanged && result.Consequence == "" {
		line += " URL changed."
	}
	if result.Snapshot != nil {
		return line + "\n\n" + ensureTrailingNewline(result.Snapshot.FormattedText)
	}
	return line + "\n"
}

func newHoverCmd() *cobra.Command {
	var snap bool
	var seed string
	cmd := &cobra.Command{
		Use:   "hover <id | text | css=selector>",
		Short: "Move the humanized cursor over an element without clicking",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := strings.TrimSpace(args[0])
			params := protocol.ActionHoverParams{Snap: snap, Seed: seed}
			if id, err := strconv.Atoi(arg); err == nil {
				if id <= 0 {
					return fmt.Errorf("invalid element id %q: must be a positive integer", arg)
				}
				params.ID = id
			} else if selector, ok := strings.CutPrefix(arg, "css="); ok {
				if strings.TrimSpace(selector) == "" {
					return errors.New("css selector cannot be empty")
				}
				params.Selector = selector
			} else {
				if arg == "" {
					return errors.New("target cannot be empty")
				}
				params.Text = arg
			}
			var result protocol.ActionHoverResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionHover, params, &result, clickTimeout); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if result.Target != nil && result.Coordinates != nil {
				fmt.Fprintf(out, "OK: hovered [%d] <%s> %q @%d,%d (%.2fs)\n",
					result.Target.ID, result.Target.Tag, result.Target.Label,
					result.Coordinates.X, result.Coordinates.Y, float64(result.DurationMs)/1000)
			} else {
				fmt.Fprintf(out, "OK: hovered (%.2fs)\n", float64(result.DurationMs)/1000)
			}
			if result.Snapshot != nil {
				fmt.Fprintf(out, "\n%s", ensureTrailingNewline(result.Snapshot.FormattedText))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a diff snapshot after hovering")
	cmd.Flags().StringVar(&seed, "seed", "", "Deterministic seed for cursor trajectory")
	return cmd
}

func newDragCmd() *cobra.Command {
	var snap bool
	var seed string
	cmd := &cobra.Command{
		Use:   "drag <from_id> <to_id>",
		Short: "Drag one element onto another with a humanized pointer path",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			fromID, err := parseActionID(args[0])
			if err != nil {
				return err
			}
			toID, err := parseActionID(args[1])
			if err != nil {
				return err
			}
			params := protocol.ActionDragParams{FromID: fromID, ToID: toID, Snap: snap, Seed: seed}
			var result protocol.ActionDragResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionDrag, params, &result, clickTimeout); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "OK: dragged [%d] -> [%d] (%.2fs)\n", fromID, toID, float64(result.DurationMs)/1000)
			if result.Snapshot != nil {
				fmt.Fprintf(out, "\n%s", ensureTrailingNewline(result.Snapshot.FormattedText))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a diff snapshot after dragging")
	cmd.Flags().StringVar(&seed, "seed", "", "Deterministic seed for drag trajectory")
	return cmd
}

func newSelectCmd() *cobra.Command {
	var snap bool
	cmd := &cobra.Command{
		Use:   "select <id> <value_or_label>",
		Short: "Select an option on a native <select> element by value or visible label",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseActionID(args[0])
			if err != nil {
				return err
			}
			params := protocol.ActionSelectParams{ID: id, Value: args[1], Snap: snap}
			var result protocol.ActionSelectResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionSelect, params, &result, clickTimeout); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			status := "unverified"
			if result.Verified {
				status = "verified"
			}
			if result.Label != "" {
				fmt.Fprintf(out, "OK: selected [%d] %q (value=%q, %s)\n", id, result.Label, result.Value, status)
			} else {
				fmt.Fprintf(out, "OK: selected [%d] value=%q (%s)\n", id, result.Value, status)
			}
			if result.Snapshot != nil {
				fmt.Fprintf(out, "\n%s", ensureTrailingNewline(result.Snapshot.FormattedText))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a diff snapshot after selecting")
	return cmd
}
