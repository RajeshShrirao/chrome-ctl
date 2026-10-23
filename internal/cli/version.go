package cli

import (
	"fmt"

	"github.com/rajeshshrirao/chrome-ctl/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the chrome-ctl version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "chrome-ctl %s\n", buildinfo.Version)
			return nil
		},
	}
}
