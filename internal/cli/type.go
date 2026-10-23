package cli

import (
	"errors"
	"fmt"
	"math"
	"time"
	"unicode/utf8"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	typeTimeout  = 310 * time.Second
	maxTypeChars = 2000
)

func newTypeCmd() *cobra.Command {
	var enter, snap bool
	var typoRate float64
	cmd := &cobra.Command{
		Use:   `type <id> "<text>"`,
		Short: "Type text into an indexed element with human cadence",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseActionID(args[0])
			if err != nil {
				return err
			}

			text := args[1]
			if n := utf8.RuneCountInString(text); n > maxTypeChars {
				return fmt.Errorf("text is too long: %d characters (maximum %d)", n, maxTypeChars)
			}
			if text == "" && !enter {
				return errors.New("text is empty")
			}
			if math.IsNaN(typoRate) || typoRate < 0 || typoRate > 1 {
				return fmt.Errorf("invalid --typo-rate %g: must be between 0 and 1", typoRate)
			}

			rate := typoRate
			params := protocol.ActionTypeParams{
				ID:       id,
				Text:     text,
				Enter:    enter,
				TypoRate: &rate,
				Snap:     snap,
			}
			var result protocol.ActionTypeResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionType, params, &result, typeTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatType(result, id))
			return nil
		},
	}
	cmd.Flags().BoolVar(&enter, "enter", false, "Press Enter after typing")
	cmd.Flags().Float64Var(&typoRate, "typo-rate", 0.02, "Fraction of characters typed with a typo (0-1)")
	cmd.Flags().BoolVar(&snap, "snap", false, "Include a page snapshot after typing")
	return cmd
}

func formatType(result protocol.ActionTypeResult, id int) string {
	cadence := fmt.Sprintf("human cadence: %.1fs", float64(result.DurationMs)/1000)
	if result.TyposInjected > 0 {
		word := "typo"
		if result.TyposInjected != 1 {
			word = "typos"
		}
		cadence += fmt.Sprintf(", %d %s", result.TyposInjected, word)
	}

	line := fmt.Sprintf("OK: Typed %d chars into [%d] (%s).", result.CharsTyped, id, cadence)
	if result.StateDelta != "" {
		line += fmt.Sprintf(" State changed: %s.", result.StateDelta)
	}
	if result.Snapshot != nil {
		return line + "\n\n" + ensureTrailingNewline(result.Snapshot.FormattedText)
	}
	return line + "\n"
}
