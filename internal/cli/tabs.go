package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newTabsCmd() *cobra.Command {
	var windowID int
	cmd := &cobra.Command{
		Use:   "tabs",
		Short: "List or close open tabs",
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
	cmd.AddCommand(newTabsCloseCmd())
	return cmd
}

func newTabsCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <tab_id>",
		Short: "Close a specific tab by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tabID, err := strconv.Atoi(strings.TrimSpace(args[0]))
			if err != nil || tabID <= 0 {
				return fmt.Errorf("invalid tab_id %q: must be a positive integer", args[0])
			}
			var result protocol.TabCloseResult
			if err := callRPC(cmd, protocol.MethodTabClose, protocol.TabCloseParams{TabID: tabID}, &result); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK: closed tab [%d]\n", result.TabID)
			return nil
		},
	}
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
