package cli

import (
	"fmt"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	var newTab bool
	cmd := &cobra.Command{
		Use:   "open <url>",
		Short: "Open a URL in the active tab or a new tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := strings.TrimSpace(args[0])
			if url == "" {
				return fmt.Errorf("url must not be empty")
			}

			params := protocol.TabOpenParams{URL: url, NewTab: newTab}
			var result protocol.TabOpenResult
			if err := callRPC(cmd, protocol.MethodTabOpen, params, &result); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatOpen(result, newTab))
			return nil
		},
	}
	cmd.Flags().BoolVar(&newTab, "new", false, "Open the URL in a new tab")
	return cmd
}

func formatOpen(result protocol.TabOpenResult, newTab bool) string {
	action := "navigated"
	if newTab {
		action = "opened"
	}

	title := result.Title
	if title == "" {
		title = "(untitled)"
	}

	line := fmt.Sprintf("OK: %s [%d] %s - %s", action, result.TabID, title, result.URL)
	if !result.Loaded {
		line += " (still loading)"
	}
	return line + "\n"
}
