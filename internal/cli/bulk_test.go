package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// newBulkServer answers the bulk RPC sequence and echoes the requested URL.
func newBulkServer(t *testing.T, token, markdown string) *httptest.Server {
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

		w.Header().Set("Content-Type", "application/json")
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
