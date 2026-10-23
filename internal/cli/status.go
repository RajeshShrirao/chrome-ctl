package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/client"
	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon and Chrome extension status",
		Args:  cobra.NoArgs,
		RunE:  runStatus,
	}
}

func runStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	addr := config.DefaultAddr()
	paths := config.DefaultPaths()

	token, err := config.ReadToken(paths.TokenFile)
	if err != nil {
		writeStatus(out, addr, false, false, 0)
		return &ExitError{Code: 1, Silent: true}
	}

	probeCtx, cancelProbe := context.WithTimeout(cmd.Context(), time.Second)
	healthy := healthCheck(probeCtx, addr, token)
	cancelProbe()
	if !healthy {
		writeStatus(out, addr, false, false, 0)
		return &ExitError{Code: 1, Silent: true}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer cancel()
	status, err := client.New(addr, token).Status(ctx)
	if err != nil {
		writeStatus(out, addr, true, false, 0)
		return err
	}

	writeStatus(out, addr, true, status.ExtensionConnected, status.TabCount)
	return nil
}

func writeStatus(w io.Writer, addr string, running, connected bool, tabs int) {
	daemonState := "STOPPED"
	if running {
		daemonState = "RUNNING"
	}
	extensionState := "DISCONNECTED"
	if connected {
		extensionState = "CONNECTED"
	}
	fmt.Fprintf(w, "Daemon: %s (%s)\nChrome Extension: %s\nActive Tabs: %d\n",
		daemonState, addr, extensionState, tabs)
}
