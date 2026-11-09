package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	macroStartTimeout = 10 * time.Second
	macroSaveTimeout  = 10 * time.Second
	macroListTimeout  = 10 * time.Second
	// Macros can type for minutes, so replay gets a much larger budget.
	macroRunTimeout = 600 * time.Second

	defaultMacroVars = "{}"
)

func newMacroCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "macro",
		Short: "Record, save, list, and replay local macros",
	}
	cmd.AddCommand(
		newMacroStartCmd(),
		newMacroSaveCmd(),
		newMacroListCmd(),
		newMacroRunCmd(),
	)
	return cmd
}

func newMacroStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start recording CLI actions into a macro",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := protocol.MacroStartParams{Name: args[0]}
			var result protocol.MacroStartResult
			if err := callRPCWithTimeout(cmd, protocol.MethodMacroStart, params, &result, macroStartTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatMacroStart(result))
			return nil
		},
	}
	return cmd
}

func newMacroSaveCmd() *cobra.Command {
	var paramsRaw, description string
	cmd := &cobra.Command{
		Use:   "save",
		Short: "Stop recording and save the macro to disk",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := protocol.MacroSaveParams{
				Params:      parseMacroParams(paramsRaw),
				Description: description,
			}
			var result protocol.MacroSaveResult
			if err := callRPCWithTimeout(cmd, protocol.MethodMacroSave, params, &result, macroSaveTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatMacroSave(result))
			return nil
		},
	}
	cmd.Flags().StringVar(&paramsRaw, "params", "", "Comma-separated parameter names to template (e.g. \"search_term,message\")")
	cmd.Flags().StringVar(&description, "description", "", "Human-readable macro description")
	return cmd
}

func newMacroListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved macros",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var result protocol.MacroListResult
			if err := callRPCWithTimeout(cmd, protocol.MethodMacroList, nil, &result, macroListTimeout); err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), formatMacroList(result.Macros))
			return nil
		},
	}
	return cmd
}

func newMacroRunCmd() *cobra.Command {
	var varsRaw string
	var runID string
	var resume bool
	cmd := &cobra.Command{
		Use:   "run <name>",
		Short: "Replay a saved macro with parameter substitution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vars, err := parseMacroVars(varsRaw)
			if err != nil {
				return err
			}
			return runMacro(cmd, args[0], vars, runID, resume)
		},
	}
	cmd.Flags().StringVar(&varsRaw, "vars", defaultMacroVars, "JSON object of macro parameter values")
	cmd.Flags().StringVar(&runID, "run-id", "", "Execution run identifier for checkpointing and resuming")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume from the last completed step in the run checkpoint")
	return cmd
}

// runMacro calls macro.run directly (rather than via callRPCWithTimeout) so that
// a step failure's structured error data can be turned into a clean snap.
func runMacro(cmd *cobra.Command, name string, vars map[string]string, runID string, resume bool) error {
	c, err := newRPCClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), macroRunTimeout)
	defer cancel()

	params := protocol.MacroRunParams{Name: name, Vars: vars, RunID: runID, Resume: resume}
	raw, rpcErr, err := c.Call(ctx, protocol.MethodMacroRun, params)
	if err != nil {
		return fmt.Errorf("%s: %w", protocol.MethodMacroRun, err)
	}
	if rpcErr != nil {
		if rpcErr.Code == protocol.ErrMacroStepFailed {
			if failure, ok := decodeMacroStepFailure(rpcErr.Data); ok {
				fmt.Fprint(cmd.ErrOrStderr(), formatMacroStepFailure(failure))
				return &ExitError{Code: 1, Silent: true}
			}
		}
		return mapRPCError(rpcErr)
	}

	var result protocol.MacroRunResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode %s result: %w", protocol.MethodMacroRun, err)
	}
	fmt.Fprint(cmd.OutOrStdout(), formatMacroRun(result))
	return nil
}

// parseMacroParams splits a comma-separated parameter list, trimming spaces and
// dropping empty entries. A blank list yields nil.
func parseMacroParams(raw string) []string {
	var params []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		params = append(params, part)
	}
	return params
}

// parseMacroVars decodes --vars into a string map. Non-string values fail.
func parseMacroVars(raw string) (map[string]string, error) {
	var vars map[string]string
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		return nil, fmt.Errorf("invalid --vars JSON: %w", err)
	}
	if vars == nil {
		vars = map[string]string{}
	}
	return vars, nil
}

// decodeMacroStepFailure re-encodes the RPC error data so it can be decoded
// into the typed failure payload regardless of how the daemon encoded it.
func decodeMacroStepFailure(data any) (protocol.MacroStepFailure, bool) {
	var failure protocol.MacroStepFailure
	if data == nil {
		return failure, false
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return failure, false
	}
	if err := json.Unmarshal(raw, &failure); err != nil {
		return failure, false
	}
	return failure, true
}

func formatMacroStart(result protocol.MacroStartResult) string {
	return fmt.Sprintf("OK: recording macro %q\n", result.Name)
}

func formatMacroSave(result protocol.MacroSaveResult) string {
	out := fmt.Sprintf("OK: saved macro %q (%d %s", result.Name, result.Steps, macroStepLabel(result.Steps))
	if len(result.Parameters) > 0 {
		out += fmt.Sprintf(", params: %s", strings.Join(result.Parameters, ", "))
	}
	return out + ")\n"
}

func formatMacroList(macros []protocol.MacroSummary) string {
	if len(macros) == 0 {
		return "No macros saved.\n"
	}

	var b strings.Builder
	b.WriteString("MACROS\n")
	for _, macro := range macros {
		fmt.Fprintf(&b, "%s - %d %s", macro.Name, macro.Steps, macroStepLabel(macro.Steps))
		if len(macro.Parameters) > 0 {
			fmt.Fprintf(&b, " [params: %s]", strings.Join(macro.Parameters, ", "))
		}
		if strings.TrimSpace(macro.Description) != "" {
			fmt.Fprintf(&b, " - %s", macro.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatMacroRun(result protocol.MacroRunResult) string {
	resumed := ""
	if result.ResumedFrom > 0 {
		resumed = fmt.Sprintf(", resumed from step %d", result.ResumedFrom)
	}
	return fmt.Sprintf("OK: ran macro %q (%d steps, %.1fs%s)\n",
		result.Macro, result.StepsRun, float64(result.DurationMs)/1000, resumed)
}

func formatMacroStepFailure(failure protocol.MacroStepFailure) string {
	out := fmt.Sprintf("MACRO STEP FAILED: macro %q step %d: %s\n",
		failure.Macro, failure.FailedStepID, failure.Reason)
	if failure.DeadLetter != "" {
		out += fmt.Sprintf("dead letter: %s\n", failure.DeadLetter)
	}
	if len(failure.Compensated) > 0 {
		out += fmt.Sprintf("compensated: %v\n", failure.Compensated)
	}
	if failure.CurrentSnapshot != "" {
		out += failure.CurrentSnapshot
		if !strings.HasSuffix(failure.CurrentSnapshot, "\n") {
			out += "\n"
		}
	}
	return out
}

func macroStepLabel(n int) string {
	if n == 1 {
		return "step"
	}
	return "steps"
}
