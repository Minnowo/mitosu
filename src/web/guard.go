// Checks every request before a handler sees it (docs/brief.md section
// 3, "Web server"): the Host header (against DNS rebinding), a custom
// header on API calls (against cross-site requests: browsers cannot send
// it cross-origin without a CORS preflight, which mitosu never answers),
// a body size cap, and security headers on every response.

package web

import (
	"net"
	"net/http"
	"strings"
)

const (
	// apiHeader must be "1" on every /api/ request.
	apiHeader = "X-Mitosu"
	// maxBody caps a request body.
	maxBody = 16 * 1024
)

// guard wraps next with the checks in the file header. allowed are the
// host names (lower case, no port) the Host header may hold besides IP
// addresses and localhost.
func guard(allowed map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")

		if !hostAllowed(r.Host, allowed) {
			http.Error(w, "unknown host name; add it as allowed_host in mitosu.conf", http.StatusMisdirectedRequest)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
			if r.Header.Get(apiHeader) != "1" {
				writeError(w, http.StatusForbidden, "missing "+apiHeader+" header")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether a Host header names this server: an IP
// address (DNS rebinding needs a name), localhost, or an allowed name.
func hostAllowed(hostHeader string, allowed map[string]bool) bool {
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil || host == "localhost" {
		return true
	}
	return allowed[host]
}
