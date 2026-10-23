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
	waitRPCTimeoutMs = 10000
	maxWaitMs        = 600000
	waitTimeout      = 30 * time.Second
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

func newWaitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wait <ms|idle|selector>",
		Short: "Wait for a duration, page idle, or a selector",
		Args:  cobra.ExactArgs(1),
		RunE:  runWait,
	}
}

func runWait(cmd *cobra.Command, args []string) error {
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
		if err := callRPCWithTimeout(cmd, protocol.MethodPageWait, params, &result, waitTimeout); err != nil {
			return err
		}
		if result.Matched {
			fmt.Fprintf(cmd.OutOrStdout(), "OK: idle after %s\n", formatWaitDuration(result.WaitedMs))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "OK: waited %s (still busy)\n", formatWaitDuration(waitRPCTimeoutMs))
		}

	case waitSelector:
		var result protocol.PageWaitResult
		params := protocol.PageWaitParams{Selector: spec.selector, TimeoutMs: waitRPCTimeoutMs}
		if err := callRPCWithTimeout(cmd, protocol.MethodPageWait, params, &result, waitTimeout); err != nil {
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
