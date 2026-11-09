package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/bulk"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	bulkDefaultWait       = "6000-9000"
	bulkDefaultMinBytes   = 300
	bulkDefaultMaxRepeats = 3
	bulkNetworkTimeoutMs  = 15000
	bulkIdleTimeoutMs     = 10000
	bulkTabOpenTimeout    = 35 * time.Second
	bulkManifestName      = "manifest.jsonl"
	bulkTemplateToken     = "{{item}}"

	// Per-host politeness. The AIMD spacing only binds when --wait is shorter
	// than 1/rate; on failure it halves until the host looks healthy again.
	bulkRateMin          = 0.1
	bulkRateMax          = 4.0
	bulkRateAlpha        = 0.25
	bulkBreakerThreshold = 4
	bulkBreakerCooldown  = 30 * time.Second

	// Retries only cover transient classes, with full-jitter exponential backoff.
	bulkRetryMax        = 3
	bulkRetryBase       = 500 * time.Millisecond
	bulkRetryCap        = 8 * time.Second
	bulkNearDupDistance = 3
)

type bulkFlags struct {
	out         string
	template    string
	wait        string
	limit       int
	minBytes    int
	stopOnEmpty bool
	maxRepeats  int
	resume      bool
	quiet       bool
	networkWait bool
}

type bulkManifestEntry struct {
	Index    int    `json:"index"`
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Bytes    int    `json:"bytes"`
	File     string `json:"file,omitempty"`
	WaitedMs int64  `json:"waitedMs"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	// Hash is the exact SHA-256 of the saved markdown; SimHash enables
	// near-duplicate detection across trivial template changes.
	Hash    string `json:"hash,omitempty"`
	SimHash uint64 `json:"simHash,omitempty"`
}

func newBulkCmd() *cobra.Command {
	var flags bulkFlags
	cmd := &cobra.Command{
		Use:   "bulk <file|->",
		Short: "Fetch many URLs into markdown files without printing page bodies",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBulk(cmd, args[0], flags)
		},
	}
	cmd.Flags().StringVar(&flags.out, "out", "", "Directory for markdown files and manifest.jsonl (required)")
	cmd.Flags().StringVar(&flags.template, "template", "", "URL template; each item is escaped into {{item}}")
	cmd.Flags().StringVar(&flags.wait, "wait", bulkDefaultWait, "Jitter delay between items (ms or ms-ms)")
	cmd.Flags().IntVar(&flags.limit, "limit", 0, "Stop after this many items (0 = no limit)")
	cmd.Flags().IntVar(&flags.minBytes, "min-bytes", bulkDefaultMinBytes, "Stop when a page's markdown is shorter than this")
	cmd.Flags().BoolVar(&flags.stopOnEmpty, "stop-on-empty", true, "Stop the run when a page is shorter than --min-bytes")
	cmd.Flags().IntVar(&flags.maxRepeats, "max-repeats", bulkDefaultMaxRepeats, "Stop after this many consecutive near-duplicate pages")
	cmd.Flags().BoolVar(&flags.resume, "resume", false, "Skip URLs already marked ok in the manifest")
	cmd.Flags().BoolVar(&flags.quiet, "quiet", false, "Suppress per-item progress on stderr")
	cmd.Flags().BoolVar(&flags.networkWait, "network", true, "Wait for network quiet after each navigation")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func runBulk(cmd *cobra.Command, source string, flags bulkFlags) error {
	if strings.TrimSpace(flags.out) == "" {
		return errors.New("bulk requires --out")
	}
	if flags.limit < 0 {
		return fmt.Errorf("invalid --limit %d: must not be negative", flags.limit)
	}
	if flags.minBytes < 0 {
		return fmt.Errorf("invalid --min-bytes %d: must not be negative", flags.minBytes)
	}
	if flags.maxRepeats < 1 {
		return fmt.Errorf("invalid --max-repeats %d: must be at least 1", flags.maxRepeats)
	}

	waitSpec, err := parseWaitArg(flags.wait)
	if err != nil {
		return fmt.Errorf("invalid --wait: %w", err)
	}
	if waitSpec.mode != waitSleep {
		return fmt.Errorf("invalid --wait %q: must be a duration or range in milliseconds", flags.wait)
	}

	items, err := readBulkItems(cmd, source)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("no items in %s (blank lines and lines starting with # are ignored)", source)
	}

	if err := os.MkdirAll(flags.out, 0o755); err != nil {
		return fmt.Errorf("create --out directory: %w", err)
	}
	manifestPath := filepath.Join(flags.out, bulkManifestName)
	done, err := loadBulkManifest(manifestPath)
	if err != nil {
		return err
	}
	manifest, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	defer manifest.Close()

	var window protocol.WindowResult
	if err := callRPCWithTimeout(cmd, protocol.MethodWindowEnsure, struct{}{}, &window, bulkTabOpenTimeout); err != nil {
		return err
	}

	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	scheduler := bulk.NewScheduler(bulk.SchedulerConfig{
		MinRate:          bulkRateMin,
		MaxRate:          bulkRateMax,
		StartRate:        bulkRateMax,
		Alpha:            bulkRateAlpha,
		FailureThreshold: bulkBreakerThreshold,
		Cooldown:         bulkBreakerCooldown,
	})
	retryPolicy := bulk.RetryPolicy{Max: bulkRetryMax, Base: bulkRetryBase, Cap: bulkRetryCap}
	nearDuplicates := bulk.NewNearDuplicates(bulkNearDupDistance)

	scraped, failed, skipped := 0, 0, 0
	stopReason := ""
	lastHash := ""
	repeats := 0

	for position, item := range items {
		index := position + 1
		if flags.limit > 0 && index > flags.limit {
			stopReason = fmt.Sprintf("limit %d reached", flags.limit)
			break
		}

		target := bulkTargetURL(item, flags.template)
		if flags.resume && done[target] {
			skipped++
			continue
		}

		started := time.Now()
		entry := bulkManifestEntry{Index: index, URL: target}
		host := bulkHost(target)
		var itemErr error

		for attempt := 0; attempt <= bulkRetryMax; attempt += 1 {
			admitted, waitErr := admitBulk(cmd, scheduler, host)
			if waitErr != nil {
				return waitErr
			}
			if !admitted {
				itemErr = fmt.Errorf("circuit open for %s after %d consecutive failures", host, bulkBreakerThreshold)
				break
			}

			entry.File, entry.Title, entry.Bytes, itemErr = scrapeOne(cmd, &window, target, index, flags, stderr)
			scheduler.Done(host, itemErr == nil)
			if itemErr == nil {
				break
			}
			if attempt == bulkRetryMax || !bulkTransient(itemErr) {
				break
			}

			delay := retryPolicy.Delay(attempt, rand.Float64(), bulkRetryAfter(itemErr))
			if !flags.quiet {
				fmt.Fprintf(stderr, "retry %d/%d %s in %s: %s\n",
					attempt+1, bulkRetryMax, target, formatWaitDuration(int(delay.Milliseconds())), itemErr)
			}
			if sleepWithContext(cmd.Context(), delay) != nil {
				return cmd.Context().Err()
			}
		}

		entry.WaitedMs = time.Since(started).Milliseconds()
		entry.OK = itemErr == nil
		if itemErr != nil {
			entry.Error = itemErr.Error()
		}

		if entry.OK {
			if entry.Bytes < flags.minBytes {
				if flags.stopOnEmpty {
					stopReason = fmt.Sprintf("page %d was %d bytes (< %d)", index, entry.Bytes, flags.minBytes)
				}
			}

			entry.Hash, entry.SimHash = bulkPageHashes(flags.out, entry.File)

			// Near-duplicates are reported through an LSH index, so repeated
			// templates with slightly different text still stop the run. The
			// exact SHA stays as the fast path.
			near := false
			if entry.SimHash != 0 {
				near = nearDuplicates.Seen(entry.SimHash)
				nearDuplicates.Add(entry.SimHash)
			}
			if near || (entry.Hash != "" && entry.Hash == lastHash) {
				repeats += 1
				if repeats >= flags.maxRepeats {
					stopReason = fmt.Sprintf("%d near-duplicate pages ending at %d", repeats, index)
				}
			} else {
				repeats = 0
				lastHash = entry.Hash
			}
			scraped += 1
		} else {
			failed += 1
			fmt.Fprintf(stdout, "FAIL %03d %s: %s\n", index, target, entry.Error)
		}

		if err := appendBulkManifest(manifest, entry); err != nil {
			fmt.Fprintf(stderr, "warning: manifest append failed: %v\n", err)
		}

		if !flags.quiet && entry.OK {
			fmt.Fprintf(stderr, "[%d/%d] %s -> %s (%d bytes, %s)\n",
				index, len(items), target, entry.File, entry.Bytes, formatWaitDuration(int(entry.WaitedMs)))
		}

		if stopReason != "" {
			break
		}

		if index < len(items) && !sleepBulk(cmd, waitSpec) {
			return cmd.Context().Err()
		}
	}

	summary := fmt.Sprintf("OK: %d scraped, %d failed", scraped, failed)
	if skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", skipped)
	}
	summary += fmt.Sprintf(" -> %s (%s)", flags.out, bulkManifestName)
	if stopReason != "" {
		summary += fmt.Sprintf(" [stopped: %s]", stopReason)
	}
	fmt.Fprintln(stdout, summary)

	if failed > 0 {
		return fmt.Errorf("%d of %d items failed; see %s", failed, len(items), manifestPath)
	}
	return nil
}

// scrapeOne navigates the automation tab, waits for the page, and writes the
// markdown file. It returns the filename, title, byte count, and any failure.
func scrapeOne(
	cmd *cobra.Command,
	window *protocol.WindowResult,
	target string,
	index int,
	flags bulkFlags,
	stderr io.Writer,
) (string, string, int, error) {
	openParams := protocol.TabOpenParams{URL: target, WindowID: &window.WindowID}
	var opened protocol.TabOpenResult
	if err := callRPCWithTimeout(cmd, protocol.MethodTabOpen, openParams, &opened, bulkTabOpenTimeout); err != nil {
		return "", "", 0, err
	}

	waitBulk(cmd, flags)

	var read protocol.PageReadResult
	if err := callRPC(cmd, protocol.MethodPageRead, protocol.PageReadParams{}, &read); err != nil {
		return "", "", 0, err
	}

	title := opened.Title
	if title == "" {
		var status protocol.SessionStatus
		if err := callRPC(cmd, protocol.MethodSessionStatus, struct{}{}, &status); err == nil && status.ActiveTab != nil {
			title = status.ActiveTab.Title
		}
	}

	filename := bulkFilename(index, target)
	if err := writeFileAtomic(filepath.Join(flags.out, filename), []byte(read.Markdown)); err != nil {
		return "", title, 0, fmt.Errorf("write %s: %w", filename, err)
	}
	if title == "" && read.Markdown == "" {
		fmt.Fprintln(stderr, "warning: empty title and markdown")
	}
	return filename, title, len(read.Markdown), nil
}

// writeFileAtomic writes to a sibling temp file, fsyncs it, then renames, so an
// interrupted crawl never leaves a half-written page behind.
func writeFileAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".bulk-*.tmp")
	if err != nil {
		return err
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// bulkHost keys per-host pacing and circuit breaking. Unparseable targets share
// one bucket rather than escaping rate control.
func bulkHost(target string) string {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil || parsed.Host == "" {
		return "local"
	}
	return parsed.Host
}

// admitBulk waits for the scheduler to admit one request. Behind an open
// breaker it waits out one cooldown and then fails if no probe is granted.
func admitBulk(cmd *cobra.Command, scheduler *bulk.Scheduler, host string) (bool, error) {
	wait, ok := scheduler.Admit(host)
	if !ok {
		if wait <= 0 {
			return false, nil
		}
		if err := sleepWithContext(cmd.Context(), wait); err != nil {
			return false, err
		}
		wait, ok = scheduler.Admit(host)
		if !ok {
			return false, nil
		}
	}
	if err := sleepWithContext(cmd.Context(), wait); err != nil {
		return false, err
	}
	return true, nil
}

// bulkTransient classifies an item failure. RPC errors carry a typed code;
// transport failures (daemon restarting, reset connection) carry none and are
// worth one more attempt. A cancelled command is never retried.
func bulkTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	code, ok := rpcErrorCode(err)
	if !ok {
		return true
	}
	return bulk.IsTransientCode(code)
}

// bulkRetryAfter reads an optional server retry hint from RPC error data. The
// HTTP transport cannot see a 429 header today; this keeps the contract in the
// one place the extension could surface it.
func bulkRetryAfter(err error) time.Duration {
	var mapped *rpcMappedError
	if !errors.As(err, &mapped) {
		return 0
	}
	data, ok := mapped.data.(map[string]any)
	if !ok || len(data) == 0 {
		return 0
	}
	if raw, ok := data["retryAfterMs"]; ok {
		if value, ok := raw.(float64); ok && value > 0 {
			return time.Duration(value) * time.Millisecond
		}
	}
	raw, ok := data["retryAfter"]
	if !ok {
		return 0
	}
	switch value := raw.(type) {
	case float64:
		if value > 0 {
			return time.Duration(value * float64(time.Second))
		}
	case string:
		if parsed, parseErr := time.ParseDuration(value); parseErr == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

// bulkPageHashes returns the exact and near-duplicate hashes of a saved page.
func bulkPageHashes(dir, name string) (string, uint64) {
	if name == "" {
		return "", 0
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", 0
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), bulk.SimHash64(string(data))
}

// waitBulk waits for network quiet, falling back to DOM idle when the debugger
// is unavailable. Timeouts do not fail the item; the read happens anyway.
func waitBulk(cmd *cobra.Command, flags bulkFlags) {
	if !flags.networkWait {
		idle := protocol.PageWaitParams{Idle: true, TimeoutMs: bulkIdleTimeoutMs}
		var result protocol.PageWaitResult
		_ = callRPCWithTimeout(cmd, protocol.MethodPageWait, idle, &result, waitRPCBudget)
		return
	}

	params := protocol.PageWaitParams{
		Network:   true,
		QuietMs:   waitDefaultQuietMs,
		TimeoutMs: bulkNetworkTimeoutMs,
	}
	var result protocol.PageWaitResult
	if err := callRPCWithTimeout(cmd, protocol.MethodPageWait, params, &result, waitRPCBudget); err == nil {
		return
	}

	idle := protocol.PageWaitParams{Idle: true, TimeoutMs: bulkIdleTimeoutMs}
	_ = callRPCWithTimeout(cmd, protocol.MethodPageWait, idle, &result, waitRPCBudget)
}

func readBulkItems(cmd *cobra.Command, source string) ([]string, error) {
	var reader io.Reader
	if source == "-" {
		reader = cmd.InOrStdin()
	} else {
		file, err := os.Open(source)
		if err != nil {
			return nil, fmt.Errorf("open item file: %w", err)
		}
		defer file.Close()
		reader = file
	}

	var items []string
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		items = append(items, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read item file: %w", err)
	}
	return items, nil
}

func bulkTargetURL(item, template string) string {
	if strings.TrimSpace(template) == "" {
		return item
	}
	return strings.ReplaceAll(template, bulkTemplateToken, url.QueryEscape(item))
}

// bulkFilename builds NNN-slug.md; the index keeps files stable when resuming.
func bulkFilename(index int, rawURL string) string {
	return fmt.Sprintf("%03d-%s.md", index, bulkSlug(rawURL))
}

func bulkSlug(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if parsed, err := url.Parse(trimmed); err == nil {
		parts := []string{}
		if host := parsed.Host; host != "" && !strings.Contains(parsed.Path, host) {
			parts = append(parts, host)
		}
		parts = append(parts, parsed.Path)
		if parsed.RawQuery != "" {
			parts = append(parts, parsed.RawQuery)
		}
		trimmed = strings.Join(parts, "-")
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(trimmed) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
		if builder.Len() >= 60 {
			break
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "page"
	}
	return slug
}

func loadBulkManifest(path string) (map[string]bool, error) {
	done := make(map[string]bool)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return done, nil
		}
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var entry bulkManifestEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.OK && entry.URL != "" {
			done[entry.URL] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return done, nil
}

func appendBulkManifest(manifest *os.File, entry bulkManifestEntry) error {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := manifest.Write(append(encoded, '\n')); err != nil {
		return err
	}
	// Resume depends on a durable record; fsync per item is cheap next to a
	// page fetch and avoids losing the whole tail on a crash.
	return manifest.Sync()
}

func sleepBulk(cmd *cobra.Command, spec waitSpec) bool {
	ms := spec.minMs
	if spec.maxMs > spec.minMs {
		ms += rand.Intn(spec.maxMs - spec.minMs + 1)
	}
	return sleepWithContext(cmd.Context(), time.Duration(ms)*time.Millisecond) == nil
}
