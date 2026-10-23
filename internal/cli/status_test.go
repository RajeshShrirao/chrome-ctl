package cli

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
)

func TestWriteStatus(t *testing.T) {
	tests := []struct {
		name      string
		running   bool
		connected bool
		tabs      int
		want      string
	}{
		{
			name:      "running and connected",
			running:   true,
			connected: true,
			tabs:      3,
			want:      "Daemon: RUNNING (127.0.0.1:9229)\nChrome Extension: CONNECTED\nActive Tabs: 3\n",
		},
		{
			name:      "running and disconnected",
			running:   true,
			connected: false,
			tabs:      0,
			want:      "Daemon: RUNNING (127.0.0.1:9229)\nChrome Extension: DISCONNECTED\nActive Tabs: 0\n",
		},
		{
			name:      "stopped",
			running:   false,
			connected: false,
			tabs:      0,
			want:      "Daemon: STOPPED (127.0.0.1:9229)\nChrome Extension: DISCONNECTED\nActive Tabs: 0\n",
		},
		{
			name:      "tab count",
			running:   true,
			connected: true,
			tabs:      42,
			want:      "Daemon: RUNNING (127.0.0.1:9229)\nChrome Extension: CONNECTED\nActive Tabs: 42\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeStatus(&buf, "127.0.0.1:9229", tt.running, tt.connected, tt.tabs)
			if got := buf.String(); got != tt.want {
				t.Errorf("writeStatus() =\n%q\nwant\n%q", got, tt.want)
			}
			if lines := strings.Count(buf.String(), "\n"); lines != 3 {
				t.Errorf("line count = %d, want 3", lines)
			}
		})
	}
}

func TestStatusCommandRunningAndConnected(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakeStatusServer(t, token, `{"status":"ok","extensionConnected":true,"tabCount":3}`)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executeStatus(t)
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	want := "Daemon: RUNNING (" + addrOf(srv) + ")\nChrome Extension: CONNECTED\nActive Tabs: 3\n"
	if out != want {
		t.Errorf("status output =\n%q\nwant\n%q", out, want)
	}
}

func TestStatusCommandRunningAndDisconnected(t *testing.T) {
	_, token := setupStatusEnv(t)
	srv := fakeStatusServer(t, token, `{"status":"ok","extensionConnected":false,"tabCount":0}`)
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executeStatus(t)
	if err != nil {
		t.Fatalf("status error = %v, want nil for disconnected extension", err)
	}
	want := "Daemon: RUNNING (" + addrOf(srv) + ")\nChrome Extension: DISCONNECTED\nActive Tabs: 0\n"
	if out != want {
		t.Errorf("status output =\n%q\nwant\n%q", out, want)
	}
}

func TestStatusCommandRPCFailure(t *testing.T) {
	setupStatusEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rpc" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("CHROME_CTL_ADDR", addrOf(srv))

	out, err := executeStatus(t)
	if err == nil {
		t.Fatal("status error = nil, want RPC failure")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("status error = %q, want mention of HTTP 500", err)
	}
	want := "Daemon: RUNNING (" + addrOf(srv) + ")\nChrome Extension: DISCONNECTED\nActive Tabs: 0\n"
	if out != want {
		t.Errorf("status output =\n%q\nwant\n%q", out, want)
	}
}

func TestStatusCommandStopped(t *testing.T) {
	setupStatusEnv(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	t.Setenv("CHROME_CTL_ADDR", addr)

	out, err := executeStatus(t)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("status error = %v, want *ExitError", err)
	}
	if exitErr.Code != 1 || !exitErr.Silent {
		t.Errorf("ExitError = %+v, want code 1 and silent", exitErr)
	}
	want := "Daemon: STOPPED (" + addr + ")\nChrome Extension: DISCONNECTED\nActive Tabs: 0\n"
	if out != want {
		t.Errorf("status output =\n%q\nwant\n%q", out, want)
	}
}

func setupStatusEnv(t *testing.T) (config.Paths, string) {
	t.Helper()
	t.Setenv("CHROME_CTL_HOME", t.TempDir())
	paths := config.DefaultPaths()
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	token, _, err := config.EnsureToken(paths.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	return paths, token
}

func fakeStatusServer(t *testing.T, token, result string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q, want %q", got, "Bearer "+token)
		}
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/rpc":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":"x","result":`+result+`}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func executeStatus(t *testing.T) (string, error) {
	t.Helper()
	root := NewRoot()
	root.SetArgs([]string{"status"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	err := root.Execute()
	return out.String(), err
}

func addrOf(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}
