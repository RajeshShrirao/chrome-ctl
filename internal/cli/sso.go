package cli

import (
	"fmt"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	defaultSSOTimeoutMs = 15000
	minSSOTimeoutMs     = 1000
	maxSSOTimeoutMs     = 60000
	// ssoRPCPadding gives the daemon room to finish before the HTTP call
	// itself times out, so the extension's structured error wins the race.
	ssoRPCPadding = 10 * time.Second
)

func newSSOCmd() *cobra.Command {
	var account string
	var timeoutMs int
	cmd := &cobra.Command{
		Use:   "sso",
		Short: "Auto-complete a Google OAuth account picker popup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateSSOTimeout(timeoutMs); err != nil {
				return err
			}

			params := protocol.AuthSSOParams{Account: account, TimeoutMs: timeoutMs}
			var result protocol.AuthSSOResult
			timeout := time.Duration(timeoutMs)*time.Millisecond + ssoRPCPadding
			if err := callRPCWithTimeout(cmd, protocol.MethodAuthSSO, params, &result, timeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatSSO(result))
			return nil
		},
	}
	cmd.Flags().StringVar(&account, "account", "", "Google account email to pick (default: first available)")
	cmd.Flags().IntVar(&timeoutMs, "timeout", defaultSSOTimeoutMs, "Popup wait timeout in milliseconds (1000-60000)")
	return cmd
}

func validateSSOTimeout(timeoutMs int) error {
	if timeoutMs < minSSOTimeoutMs || timeoutMs > maxSSOTimeoutMs {
		return fmt.Errorf("timeout must be between %d and %d ms (got %d)", minSSOTimeoutMs, maxSSOTimeoutMs, timeoutMs)
	}
	return nil
}

func formatSSO(result protocol.AuthSSOResult) string {
	switch {
	case result.SelectedAccount != "" && result.RedirectCompleted:
		return fmt.Sprintf("OK: signed in as %s (redirect complete)\n", result.SelectedAccount)
	case result.RedirectCompleted:
		return "OK: OAuth completed (redirect complete)\n"
	default:
		return "OK: OAuth popup closed (redirect pending)\n"
	}
}
