// /api/session, /api/login and /api/logout: whether login is needed and
// whether this browser is logged in; logging in and out.

package web

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/rs/zerolog/log"
)

// handleSession answers {"login": login is required, "logged_in": bool}.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	required := s.sessions != nil
	writeJSON(w, http.StatusOK, map[string]bool{
		"login":     required,
		"logged_in": !required || s.sessions.valid(r),
	})
}

// handleLogin takes {"password": "..."}: 204 and a session cookie when
// right, 401 when wrong, 429 while this address is locked out after
// failures, another check is running for it or too many checks are
// queued. Behind a reverse proxy every client has the proxy's
// address and shares one limit.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "want {\"password\": \"...\"}")
		return
	}
	result, err := s.sessions.login(w, clientAddr(r), body.Password)
	switch {
	case err != nil:
		log.Error().Err(err).Msg("Login")
		writeError(w, http.StatusInternalServerError, "login failed")
	case result == loginLocked:
		writeError(w, http.StatusTooManyRequests, "too many failed logins; wait a minute")
	case result == loginBusy:
		writeError(w, http.StatusTooManyRequests, "another login attempt is still being checked")
	case result == loginWrong:
		log.Warn().Str("from", r.RemoteAddr).Msg("Failed login")
		writeError(w, http.StatusUnauthorized, "wrong password")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleLogout ends the session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.sessions != nil {
		s.sessions.logout(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}

// clientAddr is the request's remote IP address (without the port).
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
