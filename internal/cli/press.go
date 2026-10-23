package cli

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const pressTimeout = 30 * time.Second

var namedKeys = map[string]string{
	"enter":      "Enter",
	"tab":        "Tab",
	"escape":     "Escape",
	"backspace":  "Backspace",
	"delete":     "Delete",
	"space":      "Space",
	"home":       "Home",
	"end":        "End",
	"pageup":     "PageUp",
	"pagedown":   "PageDown",
	"arrowup":    "ArrowUp",
	"arrowdown":  "ArrowDown",
	"arrowleft":  "ArrowLeft",
	"arrowright": "ArrowRight",
}

func newPressCmd() *cobra.Command {
	var modifiers []string
	cmd := &cobra.Command{
		Use:   "press <key>",
		Short: "Press a key or key combination",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := normalizeKey(args[0])
			if err != nil {
				return err
			}
			mods, err := normalizeModifiers(modifiers)
			if err != nil {
				return err
			}

			params := protocol.ActionPressParams{Key: key, Modifiers: mods}
			var result protocol.ActionPressResult
			if err := callRPCWithTimeout(cmd, protocol.MethodActionPress, params, &result, pressTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatPress(result, mods))
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&modifiers, "modifier", nil, "Modifier key (ctrl, alt, shift, meta); repeatable")
	return cmd
}

// normalizeKey maps supported named keys to their canonical spelling and
// passes through single printable characters unchanged.
func normalizeKey(raw string) (string, error) {
	if canonical, ok := namedKeys[strings.ToLower(strings.TrimSpace(raw))]; ok {
		return canonical, nil
	}

	runes := []rune(raw)
	if len(runes) == 1 && unicode.IsPrint(runes[0]) {
		return string(runes[0]), nil
	}
	return "", fmt.Errorf("invalid key %q: use a named key (Enter, Tab, Escape, Backspace, Delete, Space, Home, End, PageUp, PageDown, ArrowUp, ArrowDown, ArrowLeft, ArrowRight) or a single printable character", raw)
}

// normalizeModifiers validates modifier names and returns their canonical
// spellings in the order given.
func normalizeModifiers(raw []string) ([]string, error) {
	mods := make([]string, 0, len(raw))
	for _, modifier := range raw {
		switch strings.ToLower(strings.TrimSpace(modifier)) {
		case "ctrl", "control":
			mods = append(mods, "Ctrl")
		case "alt":
			mods = append(mods, "Alt")
		case "shift":
			mods = append(mods, "Shift")
		case "meta", "cmd", "command":
			mods = append(mods, "Meta")
		default:
			return nil, fmt.Errorf("invalid modifier %q: use ctrl, alt, shift, or meta", modifier)
		}
	}
	return mods, nil
}

func formatPress(result protocol.ActionPressResult, modifiers []string) string {
	parts := make([]string, 0, len(modifiers)+1)
	parts = append(parts, modifiers...)
	parts = append(parts, result.Key)
	return "OK: pressed " + strings.Join(parts, "+") + "\n"
}
