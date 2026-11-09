package cli

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	waitRPCTimeoutMs     = 10000
	waitNetworkTimeoutMs = 15000
	waitMaxRPCms         = 30000
	waitRPCBudget        = 40 * time.Second
	waitDefaultQuietMs   = 500
	maxWaitMs            = 600000
)

type waitMode int

const (
	waitSleep waitMode = iota
	waitIdle
	waitSelector
)

type waitSpec struct {
	mode     waitMode
	minMs    int
	maxMs    int
	selector string
}

type waitFlags struct {
	network    bool
	text       string
	ignoreCase bool
	timeoutMs  int
	quietMs    int
}

func newWaitCmd() *cobra.Command {
	var flags waitFlags
	cmd := &cobra.Command{
		Use:   "wait [<ms|ms-ms|idle|selector>]",
		Short: "Wait for a duration, page idle, a selector, body text, or network quiet",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWait(cmd, args, flags)
		},
	}
	cmd.Flags().BoolVar(&flags.network, "network", false, "Wait until no requests are in flight and none have started for --quiet-ms")
	cmd.Flags().StringVar(&flags.text, "for-text", "", "Wait until this text appears in the page body")
	cmd.Flags().BoolVar(&flags.ignoreCase, "ignore-case", false, "Match --for-text case-insensitively")
	cmd.Flags().IntVar(&flags.timeoutMs, "timeout", waitNetworkTimeoutMs, "RPC wait deadline in milliseconds")
	cmd.Flags().IntVar(&flags.quietMs, "quiet-ms", waitDefaultQuietMs, "Quiet window for --network in milliseconds")
	return cmd
}

func runWait(cmd *cobra.Command, args []string, flags waitFlags) error {
	flags.text = strings.TrimSpace(flags.text)
	usesFlags := flags.network || flags.text != ""

	if len(args) == 0 && !usesFlags {
		return errors.New("wait requires a duration, idle, selector, --network, or --for-text")
	}
	if len(args) > 0 && usesFlags {
		return errors.New("wait: pass either a positional argument or --network/--for-text, not both")
	}

	if flags.timeoutMs <= 0 || flags.timeoutMs > waitMaxRPCms {
		return fmt.Errorf("invalid --timeout %d: must be between 1 and %d milliseconds", flags.timeoutMs, waitMaxRPCms)
	}
	if flags.quietMs <= 0 || flags.quietMs > waitMaxRPCms {
		return fmt.Errorf("invalid --quiet-ms %d: must be between 1 and %d milliseconds", flags.quietMs, waitMaxRPCms)
	}

	if usesFlags {
		return runConditionWait(cmd, flags)
	}

	spec, err := parseWaitArg(args[0])
	if err != nil {
		return err
	}

	switch spec.mode {
	case waitSleep:
		ms := spec.minMs
		if spec.maxMs > spec.minMs {
			ms += rand.Intn(spec.maxMs - spec.minMs + 1)
		}
		if err := sleepWithContext(cmd.Context(), time.Duration(ms)*time.Millisecond); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "OK: waited %s\n", formatWaitDuration(ms))

	case waitIdle:
		var result protocol.PageWaitResult
		params := protocol.PageWaitParams{Idle: true, TimeoutMs: waitRPCTimeoutMs}
		if err := callRPCWithTimeout(cmd, protocol.MethodPageWait, params, &result, waitRPCBudget); err != nil {
			return err
		}
		if result.Matched {
			fmt.Fprintf(cmd.OutOrStdout(), "OK: idle after %s\n", formatWaitDuration(result.WaitedMs))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "WARN: DOM idle wait timed out after %s; the page may still be fetching. Use wait --network or wait --for-text.\n", formatWaitDuration(waitRPCTimeoutMs))
		}

	case waitSelector:
		var result protocol.PageWaitResult
		params := protocol.PageWaitParams{Selector: spec.selector, TimeoutMs: waitRPCTimeoutMs}
		if err := callRPCWithTimeout(cmd, protocol.MethodPageWait, params, &result, waitRPCBudget); err != nil {
			return err
		}
		if !result.Matched {
			return fmt.Errorf("timed out after %s waiting for %q", formatWaitDuration(waitRPCTimeoutMs), spec.selector)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "OK: selector %q appeared after %s\n",
			spec.selector, formatWaitDuration(result.WaitedMs))
	}
	return nil
}

func runConditionWait(cmd *cobra.Command, flags waitFlags) error {
	params := protocol.PageWaitParams{
		Network:   flags.network,
		Text:      flags.text,
		TimeoutMs: flags.timeoutMs,
		QuietMs:   flags.quietMs,
	}
	if cmd.Flags().Changed("ignore-case") {
		ignoreCase := flags.ignoreCase
		params.IgnoreCase = &ignoreCase
	}

	var result protocol.PageWaitResult
	if err := callRPCWithTimeout(cmd, protocol.MethodPageWait, params, &result, waitRPCBudget); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	duration := formatWaitDuration(result.WaitedMs)

	if result.Matched {
		switch {
		case flags.network && flags.text != "":
			fmt.Fprintf(out, "OK: network quiet and text %q appeared after %s (%d requests)\n", flags.text, duration, result.Requests)
		case flags.network:
			if result.WaitedMs == 0 && result.Requests == 0 {
				fmt.Fprintln(out, "WARN: network quiet after 0ms (0 requests observed); no traffic was seen. Verify state.")
			} else {
				fmt.Fprintf(out, "OK: network quiet after %s (%d requests)\n", duration, result.Requests)
			}
		default:
			fmt.Fprintf(out, "OK: text %q appeared after %s\n", flags.text, duration)
		}
		return nil
	}

	// A timeout is unknown state, not failure: the caller must re-check rather
	// than assume the page never changed.
	switch {
	case flags.network && result.Inflight > 0:
		fmt.Fprintf(out, "WARN: network still busy after %s (%d in flight, %d requests seen). State unknown.\n", duration, result.Inflight, result.Requests)
	case flags.network && flags.text != "":
		fmt.Fprintf(out, "WARN: network quiet but text %q did not appear within %s. State unknown.\n", flags.text, duration)
	case flags.network:
		fmt.Fprintf(out, "WARN: network never settled after %s (%d requests seen). State unknown.\n", duration, result.Requests)
	default:
		fmt.Fprintf(out, "WARN: text %q did not appear within %s. State unknown.\n", flags.text, duration)
	}
	return nil
}

// parseWaitArg classifies the wait argument as a fixed delay, a random delay
// range, an idle wait, or a CSS selector.
func parseWaitArg(arg string) (waitSpec, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return waitSpec{}, errors.New("wait argument must not be empty")
	}

	if isAllDigits(arg) {
		ms, err := strconv.Atoi(arg)
		if err != nil || ms > maxWaitMs {
			return waitSpec{}, fmt.Errorf("invalid wait duration %q: must be between 0 and %d milliseconds", arg, maxWaitMs)
		}
		return waitSpec{mode: waitSleep, minMs: ms, maxMs: ms}, nil
	}

	if parts := strings.Split(arg, "-"); len(parts) == 2 && isAllDigits(parts[0]) && isAllDigits(parts[1]) {
		start, startErr := strconv.Atoi(parts[0])
		end, endErr := strconv.Atoi(parts[1])
		if startErr != nil || endErr != nil || end > maxWaitMs {
			return waitSpec{}, fmt.Errorf("invalid wait range %q: must be between 0 and %d milliseconds", arg, maxWaitMs)
		}
		if start >= end {
			return waitSpec{}, fmt.Errorf("invalid wait range %q: start must be less than end", arg)
		}
		return waitSpec{mode: waitSleep, minMs: start, maxMs: end}, nil
	}

	if strings.EqualFold(arg, "idle") {
		return waitSpec{mode: waitIdle}, nil
	}

	if strings.HasPrefix(arg, "-") && isAllDigits(arg[1:]) {
		return waitSpec{}, fmt.Errorf("invalid wait duration %q: must be between 0 and %d milliseconds", arg, maxWaitMs)
	}

	return waitSpec{mode: waitSelector, selector: arg}, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// formatWaitDuration renders milliseconds as "Nms" below one second and as
// seconds with one decimal otherwise.
func formatWaitDuration(ms int) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}
