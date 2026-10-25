package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newClickAtCmd() *cobra.Command {
	var snap, double bool
	cmd := &cobra.Command{
		Use:   "clickat X,Y",
		Short: "Click exact viewport CSS pixels (from snap output)",
		Long: "Click a point given in viewport CSS pixels, exactly as `snap` prints them.\n" +
			"These are not screenshot pixels: shots are resized before they are saved.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			x, y, err := parseCoordinates(args[0])
			if err != nil {
				return err
			}
			params := protocol.ActionClickParams{
				At:          &protocol.ActionCoordinates{X: x, Y: y},
				DoubleClick: double,
				Snap:        snap,
			}
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

func parseCoordinates(arg string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(arg), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid coordinates %q: use X,Y (for example 415,228)", arg)
	}
	x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errX != nil || errY != nil || x < 0 || y < 0 {
		return 0, 0, fmt.Errorf("invalid coordinates %q: X and Y must be non-negative integers", arg)
	}
	return x, y, nil
}
