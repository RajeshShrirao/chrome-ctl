package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const netDefaultLimit = 50

func newNetCmd() *cobra.Command {
	var errorsOnly, asJSON bool
	var since string
	var limit int
	cmd := &cobra.Command{
		Use:   "net",
		Short: "Show requests observed while the debugger was attached",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceMs, err := parseSince(since)
			if err != nil {
				return err
			}
			if limit <= 0 {
				return fmt.Errorf("invalid --limit %d: must be positive", limit)
			}

			params := protocol.PageNetParams{
				ErrorsOnly: errorsOnly,
				Limit:      limit,
				SinceMs:    sinceMs,
			}
			var result protocol.PageNetResult
			if err := callRPC(cmd, protocol.MethodPageNet, params, &result); err != nil {
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

			if len(result.Entries) == 0 {
				empty := "OK: no requests recorded"
				if errorsOnly {
					empty = "OK: no failed requests recorded"
				}
				if result.Note != "" {
					fmt.Fprintf(out, "%s (%s)\n", empty, result.Note)
				} else {
					fmt.Fprintln(out, empty)
				}
				return nil
			}

			for _, entry := range result.Entries {
				status := "-"
				if entry.Status > 0 {
					status = fmt.Sprintf("%d", entry.Status)
				} else if entry.Error != "" {
					status = "ERR"
				}
				row := fmt.Sprintf("%s\t%s\t%s\t%s",
					status,
					orDash(entry.Method),
					fmt.Sprintf("%dms", entry.DurationMs),
					entry.URL,
				)
				if entry.Error != "" {
					row += "\t" + entry.Error
				}
				fmt.Fprintln(out, row)
			}
			if result.Note != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "note: %s\n", result.Note)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&errorsOnly, "errors", false, "Show only failed requests (network error or status >= 400)")
	cmd.Flags().StringVar(&since, "since", "", "Only show requests started within this window (e.g. 30s, 5m)")
	cmd.Flags().IntVar(&limit, "limit", netDefaultLimit, "Maximum entries to print")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the raw result as JSON")
	return cmd
}

func parseSince(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid --since %q: use a positive duration such as 30s or 5m", value)
	}
	return duration.Milliseconds(), nil
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
