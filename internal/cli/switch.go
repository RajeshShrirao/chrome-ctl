package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch <tab_id>",
		Short: "Switch to a tab by id",
		Args:  cobra.ExactArgs(1),
		RunE:  runSwitch,
	}
}

func runSwitch(cmd *cobra.Command, args []string) error {
	tabID, err := strconv.Atoi(strings.TrimSpace(args[0]))
	if err != nil || tabID <= 0 {
		return fmt.Errorf("invalid tab id %q: must be a positive integer", args[0])
	}

	var result protocol.TabSwitchResult
	if err := callRPC(cmd, protocol.MethodTabSwitch, protocol.TabSwitchParams{TabID: tabID}, &result); err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("failed to switch to tab [%d]", tabID)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "OK: switched to tab [%d]\n", tabID)
	return nil
}
