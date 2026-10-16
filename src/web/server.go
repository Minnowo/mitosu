// The HTTP server of `mitosu serve`: routes, login gate, timeouts, opt-in
// TLS and shutdown (docs/brief.md section 3, "Web server", "Login",
// "TLS"). The JSON API is under /api/; the built web UI is served from /.

package web

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	stdlog "log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"mitosu/src/auth"
	"mitosu/src/scheduler"
	"mitosu/src/store"
)

// Options configure the server.
type Options struct {
	// Listen is host:port.
	Listen string
	// AllowedHosts are extra names for the Host header.
	AllowedHosts []string
	// Verifier checks the login password; nil turns login off.
	Verifier *auth.Verifier
	// TLSCert and TLSKey, both set, serve HTTPS.
	TLSCert, TLSKey string
	// Tiers are the configured storage tiers (for choosing what to read).
	Tiers []store.Tier
	// PullInterval is the configured time between pulls.
	PullInterval time.Duration
	// UI holds the built web UI (index.html at its root); nil serves
	// none.
	UI fs.FS
}

// Server answers the web UI's requests.
type Server struct {
	db       *store.DB
	sched    *scheduler.Scheduler
	opts     Options
	sessions *sessions
	now      func() time.Time
}

// New returns a server reading db and sched with opts.
func New(db *store.DB, sched *scheduler.Scheduler, opts Options) *Server {
	s := &Server{db: db, sched: sched, opts: opts, now: time.Now}
	if opts.Verifier != nil {
		s.sessions = newSessions(opts.Verifier, opts.TLSCert != "")
	}
	return s
}

// Handler returns the server's routes behind the guard and login gate.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.Handle("GET /api/hosts", s.loggedIn(s.handleHosts))
	mux.Handle("GET /api/series", s.loggedIn(s.handleSeries))
	mux.Handle("GET /api/points", s.loggedIn(s.handlePoints))
	mux.Handle("GET /api/events", s.loggedIn(s.handleEvents))
	mux.Handle("POST /api/pull", s.loggedIn(s.handlePull))
	mux.Handle("GET /api/latest", s.loggedIn(s.handleLatest))
	mux.Handle("GET /api/chart", s.loggedIn(s.handleChart))
	mux.Handle("GET /api/containers", s.loggedIn(s.handleContainers))
	mux.Handle("/", fallback(s.opts.UI))

	allowed := map[string]bool{}
	if host, _, err := net.SplitHostPort(s.opts.Listen); err == nil && host != "" {
		allowed[strings.ToLower(host)] = true
	}
	for _, h := range s.opts.AllowedHosts {
		allowed[strings.ToLower(strings.TrimSuffix(h, "."))] = true
	}
	return guard(allowed, mux)
}

// Listen opens opts.Listen, and with TLS loads the certificate, so that
// both fail before anything else starts.
func (s *Server) Listen() (net.Listener, error) {
	var tlsConfig *tls.Config
	if s.opts.TLSCert != "" {
		cert, err := tls.LoadX509KeyPair(s.opts.TLSCert, s.opts.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("TLS certificate: %w", err)
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
	}
	l, err := net.Listen("tcp", s.opts.Listen)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		return tls.NewListener(l, tlsConfig), nil
	}
	return l, nil
}

// Serve answers requests on l (from Listen) until ctx is done, then shuts
// down, giving open requests 5 s.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 * 1024,
		ErrorLog:          stdlog.New(httpLog{}, "", 0),
	}
	errc := make(chan error, 1)
	go func() {
		errc <- srv.Serve(l)
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Warn().Err(err).Msg("Web server shutdown")
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loggedIn lets a request through only with a live session, when login
// is on.
func (s *Server) loggedIn(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sessions != nil && !s.sessions.valid(r) {
			writeError(w, http.StatusUnauthorized, "log in first")
			return
		}
		next(w, r)
	})
}

// writeJSON writes v as JSON with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Debug().Err(err).Msg("Writing response")
	}
}

// writeError writes {"error": message} with status.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// httpLog sends net/http's own error lines (TLS handshake errors from
// scanners and the like) to zerolog at debug level.
type httpLog struct{}

func (httpLog) Write(p []byte) (int, error) {
	log.Debug().Str("source", "http").Msg(strings.TrimSpace(string(p)))
	return len(p), nil
}
