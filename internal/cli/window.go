package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newWindowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "window",
		Short: "Manage the dedicated background automation window",
		Long: "Manage the Chrome window used for automation. window ensure reuses or " +
			"creates an unfocused window, and later commands target it automatically " +
			"so the user's own window, tabs, and focus stay untouched.",
	}
	cmd.AddCommand(
		newWindowListCmd(),
		newWindowEnsureCmd(),
		newWindowNewCmd(),
		newWindowUseCmd(),
		newWindowClearCmd(),
		newWindowCloseCmd(),
	)
	return cmd
}

func newWindowListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List open Chrome windows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var result protocol.WindowListResult
			if err := callRPC(cmd, protocol.MethodWindowList, nil, &result); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatWindows(result.Windows))
			return nil
		},
	}
}

func tabLabel(count int) string {
	if count == 1 {
		return "1 tab"
	}
	return fmt.Sprintf("%d tabs", count)
}

func formatWindows(windows []protocol.WindowInfo) string {
	if len(windows) == 0 {
		return "No open windows.\n"
	}

	var b strings.Builder
	for _, window := range windows {
		marker := " "
		if window.Focused {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s [%d] %s", marker, window.WindowID, tabLabel(window.TabCount))
		if window.State != "" && window.State != "normal" {
			fmt.Fprintf(&b, " (%s)", window.State)
		}
		if window.Automation {
			b.WriteString(" (automation)")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func newWindowEnsureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ensure",
		Short: "Reuse or create the background automation window",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var result protocol.WindowResult
			if err := callRPC(cmd, protocol.MethodWindowEnsure, nil, &result); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatWindowResult(result))
			return nil
		},
	}
}

func newWindowNewCmd() *cobra.Command {
	var foreground bool
	cmd := &cobra.Command{
		Use:   "new [url]",
		Short: "Create a new automation window",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := protocol.WindowCreateParams{Focused: foreground}
			if len(args) == 1 {
				params.URL = strings.TrimSpace(args[0])
			}
			var result protocol.WindowResult
			if err := callRPC(cmd, protocol.MethodWindowCreate, params, &result); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatWindowResult(result))
			return nil
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Focus the new window instead of leaving it in the background")
	return cmd
}

func formatWindowResult(result protocol.WindowResult) string {
	verb := "using"
	if result.Created {
		verb = "created"
	}
	focus := "unfocused"
	if result.Focused {
		focus = "focused"
	}
	return fmt.Sprintf(
		"OK: %s automation window [%d] (tab [%d], %s, %s)\n",
		verb, result.WindowID, result.ActiveTabID, tabLabel(result.TabCount), focus,
	)
}

func newWindowUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <id|focused>",
		Short: "Target an existing window for automation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := protocol.WindowUseParams{}
			if strings.EqualFold(args[0], "focused") {
				params.Focused = true
			} else {
				id, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("window id must be a number or \"focused\", got %q", args[0])
				}
				params.WindowID = &id
			}

			var result protocol.WindowUseResult
			if err := callRPC(cmd, protocol.MethodWindowUse, params, &result); err != nil {
				return err
			}
			if result.WindowID == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "OK: automation window cleared")
				return nil
			}
			focus := "unfocused"
			if result.Focused {
				focus = "focused"
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"OK: automation window is now [%d] (tab [%d], %s, %s)\n",
				*result.WindowID, result.ActiveTabID, tabLabel(result.TabCount), focus,
			)
			return nil
		},
	}
}

func newWindowClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Stop targeting a dedicated window",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var result protocol.WindowUseResult
			if err := callRPC(cmd, protocol.MethodWindowUse, nil, &result); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "OK: automation window cleared; commands use the focused window")
			return nil
		},
	}
}

func newWindowCloseCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "close <id>",
		Short: "Close a Chrome window",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("window id must be a number, got %q", args[0])
			}
			if !force {
				var windows protocol.WindowListResult
				if err := callRPC(cmd, protocol.MethodWindowList, nil, &windows); err == nil {
					for _, window := range windows.Windows {
						if window.WindowID == id && window.Focused {
							return fmt.Errorf(
								"window %d is focused; pass --force to close the window the user is using",
								id,
							)
						}
					}
				}
			}

			var result protocol.WindowCloseResult
			if err := callRPC(cmd, protocol.MethodWindowClose, protocol.WindowCloseParams{WindowID: id}, &result); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK: closed window [%d]\n", result.WindowID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Close the window even when it currently has focus")
	return cmd
}
