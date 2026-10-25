package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	evalDefaultTimeoutMs = 5000
	evalMaxTimeoutMs     = 30_000
	evalOutputMaxChars   = 100_000
)

func newEvalCmd() *cobra.Command {
	var raw, await bool
	var timeoutMs int
	cmd := &cobra.Command{
		Use:   `eval "<expression>"`,
		Short: "Evaluate JavaScript in the active tab (CDP, bypasses page CSP)",
		Long: "Evaluate an expression in the active tab through the debugger.\n" +
			"Output is JSON; use --raw to print a string result unquoted.\n" +
			"Disable the command entirely with `chrome-ctl daemon --start --no-eval`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			expression := args[0]
			if strings.TrimSpace(expression) == "" {
				return errors.New("expression is empty")
			}
			if timeoutMs < 250 || timeoutMs > evalMaxTimeoutMs {
				return fmt.Errorf("--timeout must be between 250 and %d ms", evalMaxTimeoutMs)
			}

			params := protocol.EvalParams{
				Expression:   expression,
				AwaitPromise: await,
				TimeoutMs:    timeoutMs,
			}
			var result protocol.EvalResult
			timeout := time.Duration(timeoutMs+5_000) * time.Millisecond
			if err := callRPCWithTimeout(cmd, protocol.MethodPageEval, params, &result, timeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatEvalOutput(result, raw))
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Print a string result without JSON quoting")
	cmd.Flags().BoolVar(&await, "await", false, "Await a returned promise before printing")
	cmd.Flags().IntVar(&timeoutMs, "timeout", evalDefaultTimeoutMs, "Evaluation timeout in milliseconds")
	return cmd
}

func formatEvalOutput(result protocol.EvalResult, raw bool) string {
	out := result.JSON
	if raw {
		var text string
		if err := json.Unmarshal([]byte(result.JSON), &text); err == nil {
			out = text
		}
	}
	if result.Truncated {
		out += fmt.Sprintf("\n[truncated at %d chars]", evalOutputMaxChars)
	}
	return out + "\n"
}

func newURLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "url",
		Short: "Print the active tab URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var status protocol.SessionStatus
			if err := callRPC(cmd, protocol.MethodSessionStatus, nil, &status); err != nil {
				return err
			}
			if status.ActiveTab == nil || status.ActiveTab.URL == "" {
				return errors.New("no active tab URL (open a page first)")
			}
			fmt.Fprintln(cmd.OutOrStdout(), status.ActiveTab.URL)
			return nil
		},
	}
}

func newDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `describe <id|css=selector>`,
		Short: "Show an element's identity, attributes, and geometry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := fetchAttr(cmd, args[0])
			if err != nil {
				return err
			}
			printAttr(cmd.OutOrStdout(), result)
			return nil
		},
	}
}

func newHrefCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `href <id>`,
		Short: "Print an element's resolved absolute URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := fetchAttr(cmd, args[0])
			if err != nil {
				return err
			}
			if result.Href == "" {
				return fmt.Errorf("element has no href (tag %q)", result.Tag)
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Href)
			return nil
		},
	}
}

func fetchAttr(cmd *cobra.Command, arg string) (protocol.PageAttrResult, error) {
	params := protocol.PageAttrParams{}
	if selector, ok := strings.CutPrefix(arg, "css="); ok {
		if strings.TrimSpace(selector) == "" {
			return protocol.PageAttrResult{}, errors.New("css selector is empty")
		}
		params.Selector = selector
	} else {
		id, err := parseActionID(arg)
		if err != nil {
			return protocol.PageAttrResult{}, fmt.Errorf("%w (or use css=<selector>)", err)
		}
		params.ID = &id
	}

	var result protocol.PageAttrResult
	if err := callRPC(cmd, protocol.MethodPageAttr, params, &result); err != nil {
		return protocol.PageAttrResult{}, err
	}
	return result, nil
}

func printAttr(out io.Writer, result protocol.PageAttrResult) {
	fmt.Fprintf(out, "<%s> %q @%d,%d %dx%d\n",
		result.Tag, result.Label, result.Rect.X, result.Rect.Y, result.Rect.W, result.Rect.H)
	if result.Role != "" {
		fmt.Fprintf(out, "  role: %s\n", result.Role)
	}
	if result.Type != "" {
		fmt.Fprintf(out, "  type: %s\n", result.Type)
	}
	if result.ElementID != "" {
		fmt.Fprintf(out, "  id: %s\n", result.ElementID)
	}
	if result.Name != "" {
		fmt.Fprintf(out, "  name: %s\n", result.Name)
	}
	if result.Href != "" {
		fmt.Fprintf(out, "  href: %s\n", result.Href)
	}
	if result.Src != "" {
		fmt.Fprintf(out, "  src: %s\n", result.Src)
	}
	if result.Value != "" {
		fmt.Fprintf(out, "  value: %s\n", result.Value)
	}
	if result.State != "" {
		fmt.Fprintf(out, "  state: %s\n", result.State)
	}
	if result.Matches > 1 {
		fmt.Fprintf(out, "  matches: %d elements matched this selector\n", result.Matches)
	}
	for _, key := range sortedKeys(result.Attributes) {
		fmt.Fprintf(out, "  %s=%q\n", key, result.Attributes[key])
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
