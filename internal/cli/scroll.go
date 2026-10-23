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
	scrollTimeout       = 70 * time.Second
	defaultScrollAmount = 0.8
	maxScrollAmount     = 10
)

func newScrollCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scroll <down|up> [amount]",
		Short: "Scroll the page down or up",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := parseScrollArgs(args)
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
}

// parseScrollArgs validates the direction and optional viewport-fraction amount.
func parseScrollArgs(args []string) (protocol.ActionScrollParams, error) {
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
	return fmt.Sprintf("OK: scrolled %s %dpx to %dpx (%d%%)\n",
		direction, result.ScrolledPixels, result.NewScrollY, result.ViewportPercent)
}
