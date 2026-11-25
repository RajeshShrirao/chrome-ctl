package cli

import (
	"fmt"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newDialogCmd() *cobra.Command {
	var accept, dismiss bool
	var text, autoMode string
	cmd := &cobra.Command{
		Use:   "dialog",
		Short: "Inspect, accept/dismiss, or auto-handle JavaScript alert/confirm/prompt dialogs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if accept && dismiss {
				return fmt.Errorf("pass either --accept or --dismiss, not both")
			}

			action := ""
			if accept {
				action = "accept"
			} else if dismiss {
				action = "dismiss"
			}

			auto := strings.ToLower(strings.TrimSpace(autoMode))
			if auto != "" && auto != "accept" && auto != "dismiss" && auto != "off" {
				return fmt.Errorf("invalid --auto %q: use accept, dismiss, or off", autoMode)
			}

			params := protocol.PageDialogParams{
				Action:     action,
				PromptText: text,
				Auto:       auto,
			}
			var result protocol.PageDialogResult
			if err := callRPC(cmd, protocol.MethodPageDialog, params, &result); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if result.Handled {
				fmt.Fprintf(out, "OK: dialog %sed (auto=%s)\n", result.Action, orDash(result.Auto))
				return nil
			}
			if result.Active != nil {
				fmt.Fprintf(out, "DIALOG [%s]: %q (auto=%s)\n", result.Active.Type, result.Active.Message, orDash(result.Auto))
				return nil
			}
			fmt.Fprintf(out, "OK: no active dialog (auto=%s)\n", orDash(result.Auto))
			return nil
		},
	}
	cmd.Flags().BoolVar(&accept, "accept", false, "Accept the active JS dialog")
	cmd.Flags().BoolVar(&dismiss, "dismiss", false, "Dismiss the active JS dialog")
	cmd.Flags().StringVar(&text, "text", "", "Prompt response text when using --accept on a prompt dialog")
	cmd.Flags().StringVar(&autoMode, "auto", "", "Set per-tab auto handler: accept, dismiss, or off")
	return cmd
}
