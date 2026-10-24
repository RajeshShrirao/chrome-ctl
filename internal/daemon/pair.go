package daemon

import (
	"net"
	"net/http"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
)

// The extension has host permissions for <all_urls>, so it can fetch a token
// without a copy-paste step. The endpoint is not bearer-authenticated, so it is
// fenced in: loopback peer and Host only, a custom header that cross-site
// requests cannot send without a CORS preflight, and no browser http(s) origin.
const pairHeader = "X-Chrome-Ctl-Pair"

func (d *daemon) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(pairHeader) != "1" || !loopbackRequest(r) || !pairOriginAllowed(r.Header.Get("Origin")) {
		http.Error(w, "pairing not allowed", http.StatusForbidden)
		return
	}

	token, err := config.ReadToken(d.cfg.Paths.TokenFile)
	if err != nil {
		http.Error(w, "auth token unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Token string `json:"token"`
	}{Token: token})
}

func loopbackRequest(r *http.Request) bool {
	if !loopbackHost(r.Host) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func pairOriginAllowed(origin string) bool {
	return origin == "" || strings.HasPrefix(origin, "chrome-extension://")
}
