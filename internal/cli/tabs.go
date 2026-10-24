package cli

import (
	"fmt"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newTabsCmd() *cobra.Command {
	var windowID int
	cmd := &cobra.Command{
		Use:   "tabs",
		Short: "List open tabs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var params any
			if cmd.Flags().Changed("window") {
				params = protocol.TabListParams{WindowID: &windowID}
			}
			var result protocol.TabListResult
			if err := callRPC(cmd, protocol.MethodTabList, params, &result); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatTabs(result.Tabs))
			return nil
		},
	}
	cmd.Flags().IntVar(&windowID, "window", 0, "List tabs in a specific window instead of the automation window")
	return cmd
}

func formatTabs(tabs []protocol.TabInfo) string {
	if len(tabs) == 0 {
		return "No open tabs.\n"
	}

	var b strings.Builder
	for _, tab := range tabs {
		title := tab.Title
		if title == "" {
			title = "(untitled)"
		}
		url := tab.URL
		if url == "" {
			url = "(no url)"
		}
		fmt.Fprintf(&b, "[%d] %s - %s\n", tab.TabID, title, url)
	}
	return b.String()
}
