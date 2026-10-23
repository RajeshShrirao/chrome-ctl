package daemon

import (
	"net/http"
	"strings"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
)

func (d *daemon) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.authenticate(r) {
			token := presentedToken(r)
			d.log.Printf(
				"daemon: rejected unauthenticated request (path=%s presented=%t length=%d)",
				r.URL.Path, token != "", len(token),
			)
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (d *daemon) authenticate(r *http.Request) bool {
	token := presentedToken(r)
	return token != "" && config.TokenMatches(d.cfg.Paths.TokenFile, token)
}

func presentedToken(r *http.Request) string {
	token := bearerToken(r)
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return token
}

func bearerToken(r *http.Request) string {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return fields[1]
	}
	return ""
}
