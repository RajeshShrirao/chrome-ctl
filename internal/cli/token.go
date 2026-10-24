package cli

import (
	"fmt"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/spf13/cobra"
)

func newTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Print the daemon auth token",
		Long:  "Print the shared secret used by the daemon and extension. The extension pairs automatically; use this only for manual pairing or diagnostics.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			paths := config.DefaultPaths()
			token, err := config.ReadToken(paths.TokenFile)
			if err != nil {
				return fmt.Errorf("read auth token: %w (start the daemon first)", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), token)
			return nil
		},
	}
}
