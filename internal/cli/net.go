package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
	"github.com/spf13/cobra"
)

const (
	netDefaultLimit        = 50
	netBodyDefaultMaxChars = 100000
)

var netBlockMediaPatterns = []string{
	"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp", "*.ico", "*.svg",
	"*.woff", "*.woff2", "*.ttf", "*.eot", "*.otf",
	"*.mp4", "*.webm", "*.mp3", "*.wav", "*.avi",
}

func newNetCmd() *cobra.Command {
	var errorsOnly, asJSON, showIDs, xhrOnly bool
	var since string
	var limit int
	cmd := &cobra.Command{
		Use:   "net",
		Short: "Show requests observed while the debugger was attached",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceMs, err := parseSince(since)
			if err != nil {
				return err
			}
			if limit <= 0 {
				return fmt.Errorf("invalid --limit %d: must be positive", limit)
			}

			params := protocol.PageNetParams{
				ErrorsOnly: errorsOnly,
				Limit:      limit,
				SinceMs:    sinceMs,
			}
			var result protocol.PageNetResult
			if err := callRPC(cmd, protocol.MethodPageNet, params, &result); err != nil {
				return err
			}

			entries := result.Entries
			if xhrOnly {
				filtered := make([]protocol.NetEntry, 0, len(entries))
				for _, entry := range entries {
					t := strings.ToLower(entry.Type)
					if t == "xhr" || t == "fetch" {
						filtered = append(filtered, entry)
					}
				}
				entries = filtered
			}

			out := cmd.OutOrStdout()
			if asJSON {
				result.Entries = entries
				encoded, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", encoded)
				return nil
			}

			if len(entries) == 0 {
				empty := "OK: no requests recorded"
				if errorsOnly {
					empty = "OK: no failed requests recorded"
				} else if xhrOnly {
					empty = "OK: no XHR/Fetch requests recorded"
				}
				if result.Note != "" {
					fmt.Fprintf(out, "%s (%s)\n", empty, result.Note)
				} else {
					fmt.Fprintln(out, empty)
				}
				return nil
			}

			for _, entry := range entries {
				status := "-"
				if entry.Status > 0 {
					status = fmt.Sprintf("%d", entry.Status)
				} else if entry.Error != "" {
					status = "ERR"
				}
				var row string
				if showIDs {
					row = fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
						orDash(entry.RequestID),
						status,
						orDash(entry.Method),
						fmt.Sprintf("%dms", entry.DurationMs),
						entry.URL,
					)
				} else {
					row = fmt.Sprintf("%s\t%s\t%s\t%s",
						status,
						orDash(entry.Method),
						fmt.Sprintf("%dms", entry.DurationMs),
						entry.URL,
					)
				}
				if entry.Error != "" {
					row += "\t" + entry.Error
				}
				fmt.Fprintln(out, row)
			}
			if result.Note != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "note: %s\n", result.Note)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&errorsOnly, "errors", false, "Show only failed requests (network error or status >= 400)")
	cmd.Flags().StringVar(&since, "since", "", "Only show requests started within this window (e.g. 30s, 5m)")
	cmd.Flags().IntVar(&limit, "limit", netDefaultLimit, "Maximum entries to print")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the raw result as JSON")
	cmd.Flags().BoolVar(&showIDs, "ids", false, "Include requestId in the first column (for use with `net body`)")
	cmd.Flags().BoolVar(&xhrOnly, "xhr", false, "Show only XHR and Fetch requests")

	cmd.AddCommand(newNetBodyCmd(), newNetBlockCmd())
	return cmd
}

func newNetBodyCmd() *cobra.Command {
	var maxChars int
	var full bool
	var outPath string
	cmd := &cobra.Command{
		Use:   "body <request_id>",
		Short: "Fetch the buffered response body for a requestId seen in `net --ids`",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reqID := strings.TrimSpace(args[0])
			if reqID == "" {
				return fmt.Errorf("request_id is required")
			}
			capChars := maxChars
			if full || outPath != "" {
				capChars = 0
			}
			params := protocol.PageNetBodyParams{
				RequestID: reqID,
				MaxChars:  capChars,
			}
			var result protocol.PageNetBodyResult
			if err := callRPC(cmd, protocol.MethodPageNetBody, params, &result); err != nil {
				return err
			}

			if outPath != "" {
				var data []byte
				if result.Base64Encoded {
					decoded, err := base64.StdEncoding.DecodeString(result.Body)
					if err != nil {
						return fmt.Errorf("decode base64 body: %w", err)
					}
					data = decoded
				} else {
					data = []byte(result.Body)
				}
				if err := os.WriteFile(outPath, data, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", outPath, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "OK: saved response body %s -> %s (%d bytes)\n", reqID, outPath, len(data))
				return nil
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, result.Body)
			if result.Truncated {
				fmt.Fprintf(cmd.ErrOrStderr(), "[truncated at %d of %d bytes; pass --full or -o <file>]\n", len(result.Body), result.TotalBytes)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&maxChars, "max-chars", netBodyDefaultMaxChars, "Maximum characters to print")
	cmd.Flags().BoolVar(&full, "full", false, "Print full body without truncation")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Write raw response body to a file")
	return cmd
}

func newNetBlockCmd() *cobra.Command {
	var media, clear bool
	var patterns []string
	cmd := &cobra.Command{
		Use:   "block",
		Short: "Block matching URL patterns via CDP Network.setBlockedURLs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var urls []string
			if clear {
				urls = []string{}
			} else {
				if media {
					urls = append(urls, netBlockMediaPatterns...)
				}
				for _, p := range patterns {
					p = strings.TrimSpace(p)
					if p != "" {
						urls = append(urls, p)
					}
				}
				if len(urls) == 0 {
					return fmt.Errorf("specify --media, --pattern <glob>, or --clear")
				}
			}

			var result protocol.PageNetBlockResult
			if err := callRPC(cmd, protocol.MethodPageNetBlock, protocol.PageNetBlockParams{Patterns: urls}, &result); err != nil {
				return err
			}
			if len(result.Patterns) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "OK: cleared blocked URL patterns")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "OK: blocking %d URL patterns\n", len(result.Patterns))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&media, "media", false, "Block images, fonts, and video/audio assets")
	cmd.Flags().StringArrayVar(&patterns, "pattern", nil, "URL glob pattern to block (repeatable)")
	cmd.Flags().BoolVar(&clear, "clear", false, "Clear all blocked URL patterns")
	return cmd
}

func parseSince(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid --since %q: use a positive duration such as 30s or 5m", value)
	}
	return duration.Milliseconds(), nil
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

const (
	consoleDefaultLimit    = 50
	storageDefaultMaxChars = 200
)

func newConsoleCmd() *cobra.Command {
	var errorsOnly, clearLog, asJSON bool
	var since string
	var limit int
	cmd := &cobra.Command{
		Use:   "console",
		Short: "Show console logs and uncaught exceptions captured while the debugger was attached",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sinceMs, err := parseSince(since)
			if err != nil {
				return err
			}
			if limit <= 0 {
				return fmt.Errorf("invalid --limit %d: must be positive", limit)
			}

			params := protocol.PageConsoleParams{
				ErrorsOnly: errorsOnly,
				Limit:      limit,
				SinceMs:    sinceMs,
				Clear:      clearLog,
			}
			var result protocol.PageConsoleResult
			if err := callRPC(cmd, protocol.MethodPageConsole, params, &result); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if asJSON {
				encoded, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", encoded)
				return nil
			}

			if clearLog {
				fmt.Fprintln(out, "OK: cleared console log")
				return nil
			}
			if len(result.Entries) == 0 {
				empty := "OK: no console entries recorded"
				if errorsOnly {
					empty = "OK: no console errors recorded"
				}
				if result.Note != "" {
					fmt.Fprintf(out, "%s (%s)\n", empty, result.Note)
				} else {
					fmt.Fprintln(out, empty)
				}
				return nil
			}

			for _, entry := range result.Entries {
				loc := ""
				if entry.URL != "" {
					if entry.Line > 0 {
						loc = fmt.Sprintf("\t%s:%d", entry.URL, entry.Line)
					} else {
						loc = "\t" + entry.URL
					}
				}
				fmt.Fprintf(out, "[%s]\t%s%s\n", strings.ToUpper(entry.Level), entry.Text, loc)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&errorsOnly, "errors", false, "Show only error logs and uncaught exceptions")
	cmd.Flags().StringVar(&since, "since", "", "Only show entries logged within this window (e.g. 30s, 5m)")
	cmd.Flags().IntVar(&limit, "limit", consoleDefaultLimit, "Maximum entries to print")
	cmd.Flags().BoolVar(&clearLog, "clear", false, "Clear buffered console entries for the active tab")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the raw result as JSON")
	return cmd
}

func newStorageCmd() *cobra.Command {
	var useSession, full, asJSON bool
	cmd := &cobra.Command{
		Use:   "storage [list|get <key>|set <key> <value>|del <key>|clear]",
		Short: "Inspect or modify localStorage or sessionStorage on the active tab",
		Args:  cobra.RangeArgs(0, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			area := "local"
			if useSession {
				area = "session"
			}
			action := "list"
			var key, value string
			if len(args) > 0 {
				action = strings.ToLower(strings.TrimSpace(args[0]))
			}
			switch action {
			case "list", "clear":
				if len(args) > 1 {
					return fmt.Errorf("storage %s takes no extra arguments", action)
				}
			case "get":
				if len(args) != 2 {
					return fmt.Errorf("usage: chrome-ctl storage get <key>")
				}
				key = args[1]
			case "del", "delete":
				if len(args) != 2 {
					return fmt.Errorf("usage: chrome-ctl storage del <key>")
				}
				action = "delete"
				key = args[1]
			case "set":
				if len(args) != 3 {
					return fmt.Errorf("usage: chrome-ctl storage set <key> <value>")
				}
				key = args[1]
				value = args[2]
			default:
				return fmt.Errorf("unknown storage action %q (use list, get, set, del, or clear)", action)
			}

			maxChars := storageDefaultMaxChars
			if full {
				maxChars = 0
			}

			params := protocol.PageStorageParams{
				Area:     area,
				Action:   action,
				Key:      key,
				Value:    value,
				MaxChars: maxChars,
			}
			var result protocol.PageStorageResult
			if err := callRPC(cmd, protocol.MethodPageStorage, params, &result); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if asJSON {
				encoded, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", encoded)
				return nil
			}

			switch result.Action {
			case "get":
				if !result.Found || result.Item == nil {
					fmt.Fprintf(out, "OK: %sStorage[%q] not found\n", result.Area, key)
					return nil
				}
				suffix := ""
				if result.Item.Truncated {
					suffix = " [truncated; pass --full]"
				}
				fmt.Fprintf(out, "%s%s\n", result.Item.Value, suffix)
			case "set":
				fmt.Fprintf(out, "OK: set %sStorage[%q]\n", result.Area, key)
			case "delete":
				if result.Found {
					fmt.Fprintf(out, "OK: deleted %sStorage[%q]\n", result.Area, key)
				} else {
					fmt.Fprintf(out, "OK: %sStorage[%q] was not set\n", result.Area, key)
				}
			case "clear":
				fmt.Fprintf(out, "OK: cleared %d keys from %sStorage\n", result.Cleared, result.Area)
			default:
				if len(result.Items) == 0 {
					fmt.Fprintf(out, "OK: %sStorage is empty\n", result.Area)
					return nil
				}
				for _, item := range result.Items {
					val := item.Value
					if item.Truncated {
						val += "…"
					}
					fmt.Fprintf(out, "%s\t%s\n", item.Key, val)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&useSession, "session", false, "Target sessionStorage instead of localStorage")
	cmd.Flags().BoolVar(&full, "full", false, "Print full values without truncation")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the raw result as JSON")
	return cmd
}

func newCookiesCmd() *cobra.Command {
	var domain, targetURL string
	var showValues, asJSON bool
	cmd := &cobra.Command{
		Use:   "cookies [list|set <name> <value>|del <name>]",
		Short: "List, set, or delete cookies for the active tab via CDP (values redacted by default)",
		Args:  cobra.RangeArgs(0, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			action := "list"
			var name, value string
			if len(args) > 0 {
				action = strings.ToLower(strings.TrimSpace(args[0]))
			}
			switch action {
			case "list":
				if len(args) > 1 {
					return fmt.Errorf("cookies list takes no extra arguments")
				}
			case "set":
				if len(args) != 3 {
					return fmt.Errorf("usage: chrome-ctl cookies set <name> <value>")
				}
				name = args[1]
				value = args[2]
			case "del", "delete":
				if len(args) != 2 {
					return fmt.Errorf("usage: chrome-ctl cookies del <name>")
				}
				action = "delete"
				name = args[1]
			default:
				return fmt.Errorf("unknown cookies action %q (use list, set, or del)", action)
			}

			params := protocol.PageCookiesParams{
				Action:     action,
				Domain:     domain,
				URL:        targetURL,
				Name:       name,
				Value:      value,
				ShowValues: showValues,
			}
			var result protocol.PageCookiesResult
			if err := callRPC(cmd, protocol.MethodPageCookies, params, &result); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if asJSON {
				encoded, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", encoded)
				return nil
			}

			switch result.Action {
			case "set":
				fmt.Fprintf(out, "OK: set cookie %q\n", name)
			case "delete":
				fmt.Fprintf(out, "OK: deleted cookie %q\n", name)
			default:
				if len(result.Cookies) == 0 {
					fmt.Fprintln(out, "OK: no cookies found")
					return nil
				}
				for _, c := range result.Cookies {
					flags := make([]string, 0, 3)
					if c.Secure {
						flags = append(flags, "Secure")
					}
					if c.HTTPOnly {
						flags = append(flags, "HttpOnly")
					}
					if c.SameSite != "" {
						flags = append(flags, "SameSite="+c.SameSite)
					}
					fmt.Fprintf(out, "%s\t%s\t%s%s\t%s\n", c.Name, c.Value, c.Domain, c.Path, strings.Join(flags, ","))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Filter or target a specific cookie domain")
	cmd.Flags().StringVar(&targetURL, "url", "", "Explicit URL scope for cookie operations")
	cmd.Flags().BoolVar(&showValues, "show-values", false, "Print unredacted cookie values")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the raw result as JSON")
	return cmd
}
