package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestBulkSlug(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.test/articles/hello-world", "example-test-articles-hello-world"},
		{"http://127.0.0.1:8000/spa.html", "127-0-0-1-8000-spa-html"},
		{"https://example.test/", "example-test"},
		{"queries", "queries"},
	}
	for _, test := range tests {
		if got := bulkSlug(test.url); got != test.want {
			t.Errorf("bulkSlug(%q) = %q, want %q", test.url, got, test.want)
		}
	}
}

func TestBulkTargetURL(t *testing.T) {
	if got := bulkTargetURL("https://example.test/a", ""); got != "https://example.test/a" {
		t.Errorf("plain target = %q", got)
	}
	got := bulkTargetURL("a b&c", "https://www.bing.com/search?q={{item}}")
	want := "https://www.bing.com/search?q=a+b%26c"
	if got != want {
		t.Errorf("template target = %q, want %q", got, want)
	}
}

func TestReadBulkItemsSkipsCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.txt")
	content := "# heading\n\nhttps://a.test\n  https://b.test  \n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := readBulkItems(NewRoot(), path)
	if err != nil {
		t.Fatalf("readBulkItems: %v", err)
	}
	if len(items) != 2 || items[0] != "https://a.test" || items[1] != "https://b.test" {
		t.Errorf("items = %v", items)
	}
}

func TestBulkFilename(t *testing.T) {
	if got := bulkFilename(7, "https://example.test/a"); got != "007-example-test-a.md" {
		t.Errorf("bulkFilename = %q", got)
	}
}

func TestLoadBulkManifestOnlyTrustsOKEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, bulkManifestName)
	lines := strings.Join([]string{
		`{"index":1,"url":"https://a.test","ok":true}`,
		`{"index":2,"url":"https://b.test","ok":false,"error":"boom"}`,
		`not json`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	done, err := loadBulkManifest(path)
	if err != nil {
		t.Fatalf("loadBulkManifest: %v", err)
	}
	if !done["https://a.test"] || done["https://b.test"] {
		t.Errorf("done = %v, want only a.test", done)
	}

	missing, err := loadBulkManifest(filepath.Join(dir, "nope.jsonl"))
	if err != nil || len(missing) != 0 {
		t.Errorf("missing manifest = %v, %v", missing, err)
	}
}

func TestBulkRunWritesFilesAndManifest(t *testing.T) {
	_, token := setupStatusEnv(t)
	page := strings.Repeat("word ", 120)
	srv := newBulkServer(t, token, page)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	dir := t.TempDir()
	out := filepath.Join(dir, "pages")
	items := filepath.Join(dir, "items.txt")
	if err := os.WriteFile(items, []byte("# items\nhttps://a.test/one\nhttps://a.test/two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, err := executePhase2(t, "bulk", items, "--out", out, "--wait", "0", "--quiet")
	if err != nil {
		t.Fatalf("bulk error = %v", err)
	}
	if want := "OK: 2 scraped, 0 failed -> " + out + " (manifest.jsonl)\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	for _, name := range []string{"001-a-test-one.md", "002-a-test-two.md"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != page {
			t.Errorf("%s content mismatch", name)
		}
	}

	manifest, err := os.ReadFile(filepath.Join(out, bulkManifestName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(manifest)), "\n")
	if len(lines) != 2 {
		t.Fatalf("manifest lines = %d, want 2 (%q)", len(lines), manifest)
	}
	var entry bulkManifestEntry
	if err := json.Unmarshal([]byte(lines[1]), &entry); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if entry.URL != "https://a.test/two" || !entry.OK || entry.Index != 2 {
		t.Errorf("second entry = %+v", entry)
	}
}

func TestBulkRunResumeSkipsCompletedURLs(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := newBulkServer(t, token, strings.Repeat("word ", 120))
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	dir := t.TempDir()
	out := filepath.Join(dir, "pages")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(out, bulkManifestName),
		[]byte(`{"index":1,"url":"https://a.test/one","ok":true}`+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	items := filepath.Join(dir, "items.txt")
	if err := os.WriteFile(items, []byte("https://a.test/one\nhttps://a.test/two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, err := executePhase2(t, "bulk", items, "--out", out, "--wait", "0", "--quiet", "--resume")
	if err != nil {
		t.Fatalf("bulk error = %v", err)
	}
	if want := "OK: 1 scraped, 0 failed, 1 skipped -> " + out + " (manifest.jsonl)\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(out, "001-a-test-one.md")); !os.IsNotExist(err) {
		t.Errorf("resumed item was written again")
	}
}

func TestBulkRunStopsOnShortPage(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := newBulkServer(t, token, "short")
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	dir := t.TempDir()
	out := filepath.Join(dir, "pages")
	items := filepath.Join(dir, "items.txt")
	if err := os.WriteFile(items, []byte("https://a.test/one\nhttps://a.test/two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, err := executePhase2(t, "bulk", items, "--out", out, "--wait", "0", "--quiet")
	if err != nil {
		t.Fatalf("bulk error = %v", err)
	}
	if !strings.Contains(stdout, "1 scraped") || !strings.Contains(stdout, "stopped:") {
		t.Errorf("stdout = %q, want early stop", stdout)
	}
	if _, err := os.Stat(filepath.Join(out, "002-a-test-two.md")); !os.IsNotExist(err) {
		t.Errorf("second item should not be fetched after an empty page")
	}
}

func TestBulkRunRetriesTransientFailures(t *testing.T) {
	_, token := setupStatusEnv(t)
	page := strings.Repeat("word ", 120)
	faults := &bulkFaults{method: "tab.open", code: -32000, times: 1}
	srv := newBulkServerWithFaults(t, token, page, faults)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	dir := t.TempDir()
	out := filepath.Join(dir, "pages")
	items := filepath.Join(dir, "items.txt")
	if err := os.WriteFile(items, []byte("https://a.test/one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, err := executePhase2(t, "bulk", items, "--out", out, "--wait", "0", "--quiet")
	if err != nil {
		t.Fatalf("bulk error = %v", err)
	}
	if !strings.Contains(stdout, "OK: 1 scraped, 0 failed") {
		t.Errorf("stdout = %q, want the retry to succeed", stdout)
	}
	if got := faults.count("tab.open"); got != 2 {
		t.Errorf("tab.open attempts = %d, want 2 (initial + one retry)", got)
	}
	if _, err := os.Stat(filepath.Join(out, "001-a-test-one.md")); err != nil {
		t.Errorf("page was not written after the retry: %v", err)
	}
}

func TestBulkRunStopsOnNearDuplicatePages(t *testing.T) {
	_, token := setupStatusEnv(t)
	// Same words, different punctuation and casing: exact SHA differs, SimHash
	// does not, so the run must stop on the near-duplicate.
	page := "Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu"
	srv := newBulkServer(t, token, page+"!")
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	dir := t.TempDir()
	out := filepath.Join(dir, "pages")
	items := filepath.Join(dir, "items.txt")
	if err := os.WriteFile(items, []byte("https://a.test/one\nhttps://a.test/two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, err := executePhase2(t, "bulk", items, "--out", out, "--wait", "0", "--quiet",
		"--min-bytes", "0", "--max-repeats", "1")
	if err != nil {
		t.Fatalf("bulk error = %v", err)
	}
	if !strings.Contains(stdout, "stopped: 1 near-duplicate pages ending at 2") {
		t.Errorf("stdout = %q, want the near-duplicate stop", stdout)
	}

	manifest, err := os.ReadFile(filepath.Join(out, bulkManifestName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var entry bulkManifestEntry
	first := strings.Split(strings.TrimSpace(string(manifest)), "\n")[0]
	if err := json.Unmarshal([]byte(first), &entry); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if entry.Hash == "" || entry.SimHash == 0 {
		t.Errorf("manifest entry is missing hashes: %+v", entry)
	}
}

func TestBulkHelperClassification(t *testing.T) {
	if got := bulkHost("https://example.test:8443/a"); got != "example.test:8443" {
		t.Errorf("bulkHost = %q", got)
	}
	if got := bulkHost("not a url"); got != "local" {
		t.Errorf("bulkHost fallback = %q", got)
	}

	if bulkTransient(nil) {
		t.Error("nil error must not be transient")
	}
	if !bulkTransient(fmt.Errorf("call daemon: connection reset")) {
		t.Error("transport errors are transient")
	}
	if bulkTransient(context.Canceled) {
		t.Error("a cancelled command must not be retried")
	}
	if bulkTransient(mapRPCError(&protocol.RPCError{Code: protocol.ErrElementNotFound, Message: "gone"})) {
		t.Error("element-not-found is permanent")
	}
	if !bulkTransient(mapRPCError(&protocol.RPCError{Code: protocol.ErrBusy, Message: "busy"})) {
		t.Error("busy tabs are retried")
	}

	retryAfter := mapRPCError(&protocol.RPCError{
		Code:    protocol.ErrConnection,
		Message: "slow down",
		Data:    map[string]any{"retryAfterMs": float64(1500)},
	})
	if got := bulkRetryAfter(retryAfter); got != 1500*time.Millisecond {
		t.Errorf("retryAfterMs = %v", got)
	}
	retryAfter = mapRPCError(&protocol.RPCError{
		Code:    protocol.ErrConnection,
		Message: "slow down",
		Data:    map[string]any{"retryAfter": "2s"},
	})
	if got := bulkRetryAfter(retryAfter); got != 2*time.Second {
		t.Errorf("retryAfter duration = %v", got)
	}
	if got := bulkRetryAfter(fmt.Errorf("plain")); got != 0 {
		t.Errorf("plain error retryAfter = %v", got)
	}
}

func TestWriteFileAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.md")
	if err := writeFileAtomic(path, []byte("hello")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", entry.Name())
		}
	}
}

// bulkFaults injects transient RPC failures for the first `times` calls to a
// method, modelling a flaky extension without HTTP status codes.
type bulkFaults struct {
	method string
	code   int
	times  int

	mu   sync.Mutex
	seen map[string]int
}

func (f *bulkFaults) take(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen == nil {
		f.seen = make(map[string]int)
	}
	f.seen[method] += 1
	return method == f.method && f.seen[method] <= f.times
}

func (f *bulkFaults) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen[method]
}

// newBulkServer answers the bulk RPC sequence and echoes the requested URL.
func newBulkServer(t *testing.T, token, markdown string) *httptest.Server {
	t.Helper()
	return newBulkServerWithFaults(t, token, markdown, nil)
}

func newBulkServerWithFaults(t *testing.T, token, markdown string, faults *bulkFaults) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("bad authorization %q", r.Header.Get("Authorization"))
		}
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		if faults != nil && faults.take(req.Method) {
			_, _ = io.WriteString(w, fmt.Sprintf(
				`{"jsonrpc":"2.0","id":%q,"error":{"code":%d,"message":"simulated transient failure"}}`,
				"x", faults.code))
			return
		}

		var result any
		switch req.Method {
		case "window.ensure":
			result = map[string]any{"windowId": 7, "activeTabId": 1, "tabCount": 1, "focused": false, "created": true}
		case "tab.open":
			var params struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(req.Params, &params)
			result = map[string]any{"tabId": 1, "url": params.URL, "title": "Test page", "loaded": true}
		case "page.wait":
			result = map[string]any{"waitedMs": 5, "matched": true, "requests": 2}
		case "page.read":
			result = map[string]any{"markdown": markdown}
		default:
			result = map[string]any{}
		}

		_, _ = io.WriteString(w, fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"result":%s}`,
			"x", mustJSON(result)))
	}))
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
