package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	formStateDefaultMaxControls = 200
	formStateLabelMaxRunes      = 120
	formStateValueMaxRunes      = 200
)

type formStateFlags struct {
	json        bool
	frame       string
	full        bool
	maxControls int
	filled      bool
}

func newFormStateCmd() *cobra.Command {
	var flags formStateFlags
	cmd := &cobra.Command{
		Use:   "formstate",
		Short: "Inspect state of all form controls across same-origin frames",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFormState(cmd, flags)
		},
	}

	cmd.Flags().BoolVar(&flags.json, "json", false, "Output form controls as JSON")
	cmd.Flags().StringVar(&flags.frame, "frame", "", "Filter controls by frame name or tag")
	cmd.Flags().BoolVar(&flags.full, "full", false, "Disable label/value truncation and control count cap")
	cmd.Flags().IntVar(&flags.maxControls, "max-controls", formStateDefaultMaxControls, "Maximum number of controls to output")
	cmd.Flags().BoolVar(&flags.filled, "filled", false, "Show only controls with a non-empty value or checked state")
	return cmd
}

func runFormState(cmd *cobra.Command, flags formStateFlags) error {
	if flags.maxControls <= 0 {
		return fmt.Errorf("formstate: --max-controls must be positive")
	}

	var result protocol.PageFormStateResult
	params := protocol.PageFormStateParams{}
	if err := callRPC(cmd, protocol.MethodPageFormState, params, &result); err != nil {
		return err
	}

	controls := result.Controls
	if flags.frame != "" {
		filtered := make([]protocol.FormControlState, 0)
		target := strings.ToLower(strings.TrimSpace(flags.frame))
		for _, c := range controls {
			f := strings.ToLower(c.Frame)
			if f == target || strings.HasPrefix(f, target) || strings.Contains(f, target) {
				filtered = append(filtered, c)
			}
		}
		controls = filtered
	}

	if flags.filled {
		filtered := make([]protocol.FormControlState, 0)
		for _, c := range controls {
			if c.Checked || strings.TrimSpace(c.Value) != "" {
				filtered = append(filtered, c)
			}
		}
		controls = filtered
	}

	out := cmd.OutOrStdout()
	if flags.json {
		data, err := json.MarshalIndent(controls, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
		return nil
	}

	if len(controls) == 0 {
		fmt.Fprintln(out, "No form controls found.")
		return nil
	}

	total := len(controls)
	truncated := false
	if !flags.full && total > flags.maxControls {
		controls = controls[:flags.maxControls]
		truncated = true
	}

	for _, c := range controls {
		fmt.Fprintln(out, formatFormControlBounded(c, flags.full))
	}
	if truncated {
		fmt.Fprintf(out, "[truncated: showing %d of %d controls; use --full or --frame]\n", len(controls), total)
	}
	return nil
}

func formatFormControlBounded(c protocol.FormControlState, full bool) string {
	if !full {
		c.Label = truncateRunes(c.Label, formStateLabelMaxRunes)
		c.Value = truncateRunes(c.Value, formStateValueMaxRunes)
	}
	return formatFormControl(c)
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func formatFormControl(c protocol.FormControlState) string {
	var sb strings.Builder
	frame := c.Frame
	if frame == "" {
		frame = "top"
	}
	sb.WriteString(fmt.Sprintf("[%s] ", frame))

	if c.Type == "radio" {
		if c.Checked {
			sb.WriteString("(•) ")
		} else {
			sb.WriteString("( ) ")
		}
	} else if c.Type == "checkbox" {
		if c.Checked {
			sb.WriteString("[x] ")
		} else {
			sb.WriteString("[ ] ")
		}
	}

	if c.Type != "" && c.Type != c.Tag {
		sb.WriteString(fmt.Sprintf("<%s:%s>", c.Tag, c.Type))
	} else {
		sb.WriteString(fmt.Sprintf("<%s>", c.Tag))
	}

	if c.Name != "" {
		sb.WriteString(fmt.Sprintf(" name=%q", c.Name))
	}
	if c.ID != "" {
		sb.WriteString(fmt.Sprintf(" id=%q", c.ID))
	}
	if c.Value != "" {
		sb.WriteString(fmt.Sprintf(" value=%q", c.Value))
	}
	if c.Disabled {
		sb.WriteString(" disabled")
	}
	if c.Label != "" {
		sb.WriteString(fmt.Sprintf(" label=%q", c.Label))
	}

	return sb.String()
}
