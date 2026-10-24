package daemon

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func pairRequest(t *testing.T, srvURL string, mutate func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srvURL+"/pair", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	mutate(req)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("pair request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestPairReturnsToken(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)

	resp := pairRequest(t, srv.URL, func(r *http.Request) {
		r.Header.Set(pairHeader, "1")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if want := testToken(t, cfg); body.Token != want {
		t.Fatalf("token = %q, want %q", body.Token, want)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestPairAcceptsExtensionOrigin(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)

	resp := pairRequest(t, srv.URL, func(r *http.Request) {
		r.Header.Set(pairHeader, "1")
		r.Header.Set("Origin", "chrome-extension://abcdefghijklmnop")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPairRejectsMissingHeader(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)

	resp := pairRequest(t, srv.URL, func(*http.Request) {})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestPairRejectsWebOrigin(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)

	for _, origin := range []string{"http://evil.test", "https://evil.test", "null"} {
		resp := pairRequest(t, srv.URL, func(r *http.Request) {
			r.Header.Set(pairHeader, "1")
			r.Header.Set("Origin", origin)
		})
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: status = %d, want 403", origin, resp.StatusCode)
		}
	}
}

func TestPairRejectsNonLoopbackHost(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)

	resp := pairRequest(t, srv.URL, func(r *http.Request) {
		r.Header.Set(pairHeader, "1")
		r.Host = "evil.test:9229"
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestPairRejectsGET(t *testing.T) {
	cfg := testConfig(t)
	srv := startDaemon(t, cfg)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/pair", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set(pairHeader, "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pair request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}
