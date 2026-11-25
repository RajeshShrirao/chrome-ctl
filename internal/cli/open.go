package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	openWaitDefaultTimeoutMs = 15000
	openWaitMaxTimeoutMs     = 30000
	openWaitRPCBuffer        = 15 * time.Second
)

func newOpenCmd() *cobra.Command {
	var newTab, waitNetwork, ignoreCase, snap bool
	var windowID, timeoutMs int
	var forText string
	cmd := &cobra.Command{
		Use:   "open <url>",
		Short: "Open a URL in the active tab or a new tab",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := strings.TrimSpace(args[0])
			if url == "" {
				return fmt.Errorf("url must not be empty")
			}
			forText = strings.TrimSpace(forText)
			if cmd.Flags().Changed("for-text") && forText == "" {
				return errors.New("--for-text must not be empty")
			}
			if ignoreCase && forText == "" {
				return errors.New("--ignore-case requires --for-text")
			}
			if timeoutMs <= 0 || timeoutMs > openWaitMaxTimeoutMs {
				return fmt.Errorf("invalid --timeout %d: must be between 1 and %d milliseconds", timeoutMs, openWaitMaxTimeoutMs)
			}

			waitRequested := waitNetwork || forText != ""
			params := protocol.TabOpenParams{
				URL:            url,
				NewTab:         newTab,
				WaitNetwork:    waitNetwork,
				WaitText:       forText,
				WaitIgnoreCase: ignoreCase,
				Snap:           snap,
			}
			if waitRequested {
				params.WaitTimeoutMs = timeoutMs
			}
			if cmd.Flags().Changed("window") {
				params.WindowID = &windowID
			}

			var result protocol.TabOpenResult
			if budget, ok := openRPCTimeout(waitRequested, snap, timeoutMs); ok {
				if err := callRPCWithTimeout(cmd, protocol.MethodTabOpen, params, &result, budget); err != nil {
					return err
				}
			} else {
				if err := callRPC(cmd, protocol.MethodTabOpen, params, &result); err != nil {
					return err
				}
			}
			fmt.Fprint(cmd.OutOrStdout(), formatOpen(result, newTab))
			return nil
		},
	}
	cmd.Flags().BoolVar(&newTab, "new", false, "Open the URL in a new tab")
	cmd.Flags().IntVar(&windowID, "window", 0, "Target a specific window instead of the automation window")
	cmd.Flags().BoolVar(&waitNetwork, "wait", false, "Wait for network quiet after navigation")
	cmd.Flags().StringVar(&forText, "for-text", "", "Wait until this text appears in the page body")
	cmd.Flags().BoolVar(&ignoreCase, "ignore-case", false, "Match --for-text case-insensitively")
	cmd.Flags().IntVar(&timeoutMs, "timeout", openWaitDefaultTimeoutMs, "Wait deadline in milliseconds")
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a page snapshot after opening")
	return cmd
}

func openRPCTimeout(waitRequested, snap bool, waitTimeoutMs int) (time.Duration, bool) {
	if !waitRequested && !snap {
		return 0, false
	}
	return time.Duration(waitTimeoutMs)*time.Millisecond + openWaitRPCBuffer, true
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

	var out strings.Builder
	fmt.Fprintf(&out, "OK: %s [%d] %s - %s", action, result.TabID, title, result.URL)
	if !result.Loaded {
		out.WriteString(" (still loading)")
	}
	if result.Wait != nil {
		out.WriteByte('\n')
		out.WriteString(formatOpenWait(result.Wait))
	}
	return appendSnapshot(out.String(), result.Snapshot)
}

func formatOpenWait(wait *protocol.PageWaitResult) string {
	duration := formatWaitDuration(wait.WaitedMs)
	if wait.Matched {
		if wait.Requests > 0 {
			return fmt.Sprintf("OK: wait settled after %s (%d requests)", duration, wait.Requests)
		}
		return fmt.Sprintf("OK: wait settled after %s", duration)
	}
	switch {
	case wait.Inflight > 0:
		return fmt.Sprintf("WARN: open wait timed out after %s (%d in flight, %d requests); state may still be settling.", duration, wait.Inflight, wait.Requests)
	case wait.Requests > 0:
		return fmt.Sprintf("WARN: open wait timed out after %s (%d requests); state may still be settling.", duration, wait.Requests)
	default:
		return fmt.Sprintf("WARN: open wait timed out after %s; state may still be settling.", duration)
	}
}
