package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestFormatReadTables(t *testing.T) {
	result := protocol.PageReadResult{
		Tables: []protocol.ReadTable{
			{
				Index:   1,
				Caption: "Revenue",
				Headers: []string{"Region", "Q1"},
				Rows:    [][]string{{"North", "10"}, {"South", "20"}},
			},
			{
				Index:   2,
				Headers: []string{"Name"},
				Rows:    [][]string{{"Ada"}},
			},
		},
	}

	want := "# table 1: Revenue\nRegion\tQ1\nNorth\t10\nSouth\t20\n\n# table 2\nName\nAda\n"
	if got := formatReadTables(result); got != want {
		t.Errorf("formatReadTables = %q, want %q", got, want)
	}
}

func TestFormatReadTablesEmpty(t *testing.T) {
	if got := formatReadTables(protocol.PageReadResult{}); got != "OK: no tables found\n" {
		t.Errorf("empty tables = %q", got)
	}
}

func TestFormatReadLinks(t *testing.T) {
	result := protocol.PageReadResult{
		Links: []protocol.ReadLink{
			{Text: "Docs", Href: "https://example.test/docs"},
			{Text: "", Href: "https://example.test/icon"},
		},
	}
	if got, want := formatReadLinks(result), "Docs\thttps://example.test/docs\n\thttps://example.test/icon\n"; got != want {
		t.Errorf("formatReadLinks = %q, want %q", got, want)
	}
	if got := formatReadLinks(protocol.PageReadResult{}); got != "OK: no links found\n" {
		t.Errorf("empty links = %q", got)
	}
}

func TestReadCommandTablesMode(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 1)
	srv := fakePhase2Server(t, token, phase2Result(
		`{"markdown":"","tables":[{"index":1,"headers":["A"],"rows":[["1"]]}],"truncated":true}`,
	), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executePhase2(t, "read", "--tables", "--max-tables", "3")
	if err != nil {
		t.Fatalf("read --tables error = %v", err)
	}
	if want := "# table 1\nA\n1\n[truncated]\n"; out != want {
		t.Errorf("read --tables output = %q, want %q", out, want)
	}

	var params protocol.PageReadParams
	if err := json.Unmarshal((<-requests).Params, &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if params.Mode != "tables" || params.MaxTables != 3 {
		t.Errorf("params = %+v, want tables mode with max 3", params)
	}
}

func TestReadCommandRejectsConflictingModes(t *testing.T) {
	if _, err := executePhase2(t, "read", "--tables", "--links"); err == nil {
		t.Fatal("read --tables --links error = nil, want rejection")
	}
}

func TestReadCommandMaxChars(t *testing.T) {
	_, token := setupStatusEnv(t)
	requests := make(chan phase2Request, 5)
	srv := fakePhase2Server(t, token, phase2Result(`{"markdown":"ok"}`), requests)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "default", args: []string{"read"}, want: 20000},
		{name: "full", args: []string{"read", "--full"}, want: 100000},
		{name: "custom", args: []string{"read", "--max-chars", "5000"}, want: 5000},
		{name: "min_bound", args: []string{"read", "--max-chars", "1"}, want: 1},
		{name: "max_bound", args: []string{"read", "--max-chars", "100000"}, want: 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := executePhase2(t, tt.args...); err != nil {
				t.Fatalf("%v error = %v", tt.args, err)
			}
			var params protocol.PageReadParams
			if err := json.Unmarshal((<-requests).Params, &params); err != nil {
				t.Fatalf("decode params: %v", err)
			}
			if params.MaxChars != tt.want {
				t.Errorf("params.MaxChars = %d, want %d", params.MaxChars, tt.want)
			}
		})
	}
}

func TestReadCommandRejectsInvalidMaxChars(t *testing.T) {
	invalid := [][]string{
		{"read", "--max-chars", "0"},
		{"read", "--max-chars", "-1"},
		{"read", "--max-chars", "100001"},
		{"read", "--full", "--max-chars", "5000"},
	}
	for _, args := range invalid {
		if _, err := executePhase2(t, args...); err == nil {
			t.Errorf("%v error = nil, want rejection", args)
		}
	}
}

func TestReadCommandMarkdownTruncationMarker(t *testing.T) {
	t.Run("no duplicate when markdown already ends with [truncated]", func(t *testing.T) {
		_, token := setupStatusEnv(t)
		srv := fakePhase2Server(t, token, phase2Result(
			`{"markdown":"# Heading\n\nBody\n\n[truncated]","truncated":true}`,
		), nil)
		defer srv.Close()
		t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

		out, err := executePhase2(t, "read")
		if err != nil {
			t.Fatalf("read error = %v", err)
		}
		if want := "# Heading\n\nBody\n\n[truncated]\n"; out != want {
			t.Errorf("read output = %q, want %q", out, want)
		}
		if count := strings.Count(out, "[truncated]"); count != 1 {
			t.Errorf("[truncated] count = %d, want 1 (output: %q)", count, out)
		}
	})

	t.Run("appends [truncated] when markdown lacks suffix", func(t *testing.T) {
		_, token := setupStatusEnv(t)
		srv := fakePhase2Server(t, token, phase2Result(
			`{"markdown":"# Heading\n\nBody\n","truncated":true}`,
		), nil)
		defer srv.Close()
		t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

		out, err := executePhase2(t, "read")
		if err != nil {
			t.Fatalf("read error = %v", err)
		}
		if want := "# Heading\n\nBody\n[truncated]\n"; out != want {
			t.Errorf("read output = %q, want %q", out, want)
		}
	})
}
