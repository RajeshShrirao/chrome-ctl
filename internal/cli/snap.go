package cli

import (
	"fmt"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSnapCmd() *cobra.Command {
	var all, forms, deep, frames bool
	var maxElements int
	cmd := &cobra.Command{
		Use:   "snap",
		Short: "Snapshot interactive elements on the page",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := protocol.PageSnapParams{
				All:         all,
				FormsOnly:   forms,
				Deep:        deep,
				Frames:      frames,
				MaxElements: maxElements,
			}
			var result protocol.SnapResult
			if err := callRPC(cmd, protocol.MethodPageSnap, params, &result); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), ensureTrailingNewline(result.FormattedText))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Include all indexed elements, not just visible ones")
	cmd.Flags().BoolVar(&forms, "forms", false, "Limit the snapshot to form controls")
	cmd.Flags().BoolVar(&deep, "deep", false, "Include cursor:pointer controls and bare rows/cells")
	cmd.Flags().BoolVar(&frames, "frames", false, "Traverse all same-origin frames regardless of size or visibility")
	cmd.Flags().IntVar(&maxElements, "max-elements", 400, "Cap the number of indexed elements")
	return cmd
}

// ensureTrailingNewline leaves the text untouched but guarantees exactly one
// trailing newline.
func ensureTrailingNewline(text string) string {
	return strings.TrimRight(text, "\r\n") + "\n"
}
