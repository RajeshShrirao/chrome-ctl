package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

func newReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read [<id_or_selector>]",
		Short: "Read the page or an indexed element as markdown",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runRead,
	}
}

func runRead(cmd *cobra.Command, args []string) error {
	params, err := readParams(args)
	if err != nil {
		return err
	}

	var result protocol.PageReadResult
	if err := callRPC(cmd, protocol.MethodPageRead, params, &result); err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), trimTrailingWhitespace(result.Markdown)+"\n")
	return nil
}

// readParams classifies the optional argument: all digits mean an indexed
// element id, anything else is a CSS selector, and no argument means the
// whole page.
func readParams(args []string) (protocol.PageReadParams, error) {
	var params protocol.PageReadParams
	if len(args) == 0 {
		return params, nil
	}

	arg := args[0]
	if !isAllDigits(arg) {
		params.Selector = arg
		return params, nil
	}

	id, err := strconv.Atoi(arg)
	if err != nil {
		return params, fmt.Errorf("invalid element id %q: %w", arg, err)
	}
	params.ID = &id
	return params, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func trimTrailingWhitespace(s string) string {
	return strings.TrimRight(s, " \t\r\n")
}
