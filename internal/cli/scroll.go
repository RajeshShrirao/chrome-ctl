package cli

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	scrollTimeout       = 5 * time.Second
	defaultScrollAmount = 0.8
	maxScrollAmount     = 10
)

func newScrollCmd() *cobra.Command {
	var toSelector string
	cmd := &cobra.Command{
		Use:   "scroll <down|up> [amount] | scroll --to <selector>",
		Short: "Scroll the page down or up, or jump directly to a selector",
		Args:  cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := parseScrollWithSelector(args, toSelector)
			if err != nil {
				return err
			}

			var result protocol.ActionScrollResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionScroll, params, &result, scrollTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatScroll(result, params.Direction))
			return nil
		},
	}
	cmd.Flags().StringVar(&toSelector, "to", "", "Jump scroll directly to element matching CSS selector")
	return cmd
}

func parseScrollWithSelector(args []string, toSelector string) (protocol.ActionScrollParams, error) {
	if toSelector != "" {
		return protocol.ActionScrollParams{ToSelector: toSelector}, nil
	}
	if len(args) == 0 {
		return protocol.ActionScrollParams{}, fmt.Errorf("scroll requires direction <down|up> or --to <selector>")
	}
	return parseScrollArgs(args)
}

// parseScrollArgs validates the direction and optional viewport-fraction amount.
func parseScrollArgs(args []string) (protocol.ActionScrollParams, error) {
	if len(args) == 0 {
		return protocol.ActionScrollParams{}, fmt.Errorf("scroll requires direction <down|up>")
	}
	params := protocol.ActionScrollParams{Amount: defaultScrollAmount}

	direction := strings.ToLower(strings.TrimSpace(args[0]))
	if direction != "down" && direction != "up" {
		return params, fmt.Errorf("invalid direction %q: must be down or up", args[0])
	}
	params.Direction = direction

	if len(args) < 2 {
		return params, nil
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(args[1]), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return params, fmt.Errorf("invalid scroll amount %q: must be a number", args[1])
	}
	if amount <= 0 || amount > maxScrollAmount {
		return params, fmt.Errorf("invalid scroll amount %q: must be greater than 0 and at most %d", args[1], maxScrollAmount)
	}
	params.Amount = amount
	return params, nil
}

func formatScroll(result protocol.ActionScrollResult, direction string) string {
	if result.Target != "" {
		return fmt.Sprintf("OK: scrolled to %s\n", result.Target)
	}
	if result.Frame != "" {
		return fmt.Sprintf("OK: scrolled frame %s to %dpx\n", result.Frame, int(math.Round(result.NewScrollY)))
	}
	return fmt.Sprintf("OK: scrolled %s %dpx to %dpx (%d%%)\n",
		direction, int(math.Round(result.ScrolledPixels)), int(math.Round(result.NewScrollY)), int(math.Round(result.ViewportPercent)))
}
