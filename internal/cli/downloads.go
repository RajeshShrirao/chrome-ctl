package cli

import (
	"encoding/json"
	"fmt"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newDownloadsCmd() *cobra.Command {
	var limit int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "downloads",
		Short: "List recent browser downloads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return fmt.Errorf("invalid --limit %d: must be positive", limit)
			}
			var result protocol.PageDownloadsResult
			if err := callRPC(cmd, protocol.MethodPageDownloads, protocol.PageDownloadsParams{Limit: limit}, &result); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if asJSON {
				encoded, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", encoded)
				return nil
			}

			if len(result.Downloads) == 0 {
				fmt.Fprintln(out, "OK: no downloads found")
				return nil
			}
			for _, item := range result.Downloads {
				row := fmt.Sprintf("[%d]\t%s\t%dB\t%s", item.ID, item.State, item.Bytes, orDash(item.Filename))
				if item.Error != "" {
					row += "\t" + item.Error
				}
				fmt.Fprintln(out, row)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum downloads to list")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the raw result as JSON")
	return cmd
}
