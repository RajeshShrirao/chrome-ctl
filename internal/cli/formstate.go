package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

type formStateFlags struct {
	json  bool
	frame string
}

func newFormStateCmd() *cobra.Command {
	var flags formStateFlags
	cmd := &cobra.Command{
		Use:   "formstate",
		Short: "Inspect state of all form controls across same-origin frames",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var result protocol.PageFormStateResult
			params := protocol.PageFormStateParams{}
			if err := callRPC(cmd, protocol.MethodPageFormState, params, &result); err != nil {
				return err
			}

			controls := result.Controls
			if flags.frame != "" {
				var filtered []protocol.FormControlState
				target := strings.ToLower(strings.TrimSpace(flags.frame))
				for _, c := range controls {
					f := strings.ToLower(c.Frame)
					if f == target || strings.HasPrefix(f, target) || strings.Contains(f, target) {
						filtered = append(filtered, c)
					}
				}
				controls = filtered
			}

			if flags.json {
				data, err := json.MarshalIndent(controls, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
				return nil
			}

			out := cmd.OutOrStdout()
			if len(controls) == 0 {
				fmt.Fprintln(out, "No form controls found.")
				return nil
			}

			for _, c := range controls {
				fmt.Fprintln(out, formatFormControl(c))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&flags.json, "json", false, "Output form controls as JSON")
	cmd.Flags().StringVar(&flags.frame, "frame", "", "Filter controls by frame name or tag")
	return cmd
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
