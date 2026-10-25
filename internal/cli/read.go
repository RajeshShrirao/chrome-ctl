package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	readDefaultMaxTables = 10
	readDefaultMaxLinks  = 200
)

type readFlags struct {
	tables    bool
	links     bool
	asJSON    bool
	maxTables int
	maxLinks  int
}

func newReadCmd() *cobra.Command {
	var flags readFlags
	cmd := &cobra.Command{
		Use:   "read [<id_or_selector>]",
		Short: "Read the page or an indexed element as markdown, tables, or links",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRead(cmd, args, flags)
		},
	}
	cmd.Flags().BoolVar(&flags.tables, "tables", false, "Emit tables as TSV instead of markdown")
	cmd.Flags().BoolVar(&flags.links, "links", false, "Emit visible links as text<TAB>href")
	cmd.Flags().BoolVar(&flags.asJSON, "json", false, "Emit the structured result as JSON")
	cmd.Flags().IntVar(&flags.maxTables, "max-tables", readDefaultMaxTables, "Maximum tables in --tables mode")
	cmd.Flags().IntVar(&flags.maxLinks, "max-links", readDefaultMaxLinks, "Maximum links in --links mode")
	return cmd
}

func runRead(cmd *cobra.Command, args []string, flags readFlags) error {
	if flags.tables && flags.links {
		return fmt.Errorf("read: --tables and --links are mutually exclusive")
	}
	if flags.maxTables <= 0 || flags.maxLinks <= 0 {
		return fmt.Errorf("read: --max-tables and --max-links must be positive")
	}

	params, err := readParams(args)
	if err != nil {
		return err
	}
	switch {
	case flags.tables:
		params.Mode = "tables"
		params.MaxTables = flags.maxTables
	case flags.links:
		params.Mode = "links"
		params.MaxLinks = flags.maxLinks
	}

	var result protocol.PageReadResult
	if err := callRPC(cmd, protocol.MethodPageRead, params, &result); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if flags.asJSON {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s\n", encoded)
		return nil
	}

	switch {
	case flags.tables:
		fmt.Fprint(out, formatReadTables(result))
	case flags.links:
		fmt.Fprint(out, formatReadLinks(result))
	default:
		fmt.Fprint(out, trimTrailingWhitespace(result.Markdown)+"\n")
	}
	if result.Truncated {
		fmt.Fprintln(out, "[truncated]")
	}
	return nil
}

// formatReadTables renders each table as a comment line followed by TSV rows.
func formatReadTables(result protocol.PageReadResult) string {
	var builder strings.Builder
	for index, table := range result.Tables {
		if index > 0 {
			builder.WriteString("\n")
		}
		label := fmt.Sprintf("# table %d", table.Index)
		if table.Caption != "" {
			label += ": " + table.Caption
		}
		builder.WriteString(label + "\n")
		if len(table.Headers) > 0 {
			builder.WriteString(strings.Join(table.Headers, "\t") + "\n")
		}
		for _, row := range table.Rows {
			builder.WriteString(strings.Join(row, "\t") + "\n")
		}
	}
	if builder.Len() == 0 {
		return "OK: no tables found\n"
	}
	return builder.String()
}

func formatReadLinks(result protocol.PageReadResult) string {
	if len(result.Links) == 0 {
		return "OK: no links found\n"
	}
	var builder strings.Builder
	for _, link := range result.Links {
		builder.WriteString(link.Text + "\t" + link.Href + "\n")
	}
	return builder.String()
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
