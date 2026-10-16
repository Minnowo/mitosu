// Login sessions for the web UI: a random token in an HttpOnly,
// SameSite=Strict cookie, kept (hashed) in memory, so a restart logs
// everyone out. Failed logins are limited per client address (one
// address cannot lock out another), each address has at most one attempt
// in flight (counted before the check, so parallel requests cannot pass
// the limit), and password checks run one at a time (each takes about
// 0.05 s and 64 MiB) with at most maxWaiting queued.

package web

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"mitosu/src/auth"
)

const (
	cookieName      = "mitosu_session"
	sessionLifetime = 30 * 24 * time.Hour
	maxSessions     = 64
	// After maxFailures failed logins within failureWindow, logins are
	// refused until lockout has passed since the last failure.
	maxFailures   = 5
	failureWindow = 10 * time.Minute
	lockout       = time.Minute
	// maxWaiting caps password checks running or waiting at once.
	maxWaiting = 8
	// maxClients caps the addresses whose failures are remembered.
	maxClients = 4096
)

// sessions holds the logged-in sessions and the login rate limit.
type sessions struct {
	verifier *auth.Verifier
	secure   bool
	now      func() time.Time

	check sync.Mutex // one password check at a time

	mu      sync.Mutex
	active  map[[32]byte]time.Time // token hash -> expiry
	clients map[string]*client     // by address
	waiting int
}

// client is one address's login state.
type client struct {
	failures []time.Time
	inFlight bool
	last     time.Time
}

func newSessions(verifier *auth.Verifier, secure bool) *sessions {
	return &sessions{verifier: verifier, secure: secure, now: time.Now,
		active: map[[32]byte]time.Time{}, clients: map[string]*client{}}
}

// valid reports whether r carries a live session cookie.
func (s *sessions) valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	key := sha256.Sum256([]byte(c.Value))
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.active[key]
	if !ok {
		return false
	}
	if s.now().After(expiry) {
		delete(s.active, key)
		return false
	}
	return true
}

// loginResult is the outcome of a login attempt.
type loginResult int

const (
	loginOK loginResult = iota
	loginWrong
	loginLocked
	loginBusy
)

// login checks password for a client at addr and, if right, starts a
// session and sets its cookie on w.
func (s *sessions) login(w http.ResponseWriter, addr, password string) (loginResult, error) {
	result := s.admit(addr)
	if result != loginOK {
		return result, nil
	}
	s.check.Lock()
	ok := s.verifier.Check(password)
	s.check.Unlock()
	s.done(addr, ok)
	if !ok {
		return loginWrong, nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return loginWrong, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiry := s.now().Add(sessionLifetime)
	s.mu.Lock()
	if len(s.active) >= maxSessions {
		s.dropOldest()
	}
	s.active[sha256.Sum256([]byte(token))] = expiry
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", Expires: expiry,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
	return loginOK, nil
}

// logout ends r's session and clears the cookie.
func (s *sessions) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.active, sha256.Sum256([]byte(c.Value)))
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

// admit decides whether addr may try a password now: not while it is
// locked out or already has an attempt running, and not when too many
// checks are queued. An admitted attempt counts as running until done.
func (s *sessions) admit(addr string) loginResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	c := s.clients[addr]
	if c == nil {
		if len(s.clients) >= maxClients {
			s.forgetOldestClient()
		}
		c = &client{}
		s.clients[addr] = c
	}
	c.last = now
	kept := c.failures[:0]
	for _, f := range c.failures {
		if now.Sub(f) < failureWindow {
			kept = append(kept, f)
		}
	}
	c.failures = kept
	if len(kept) >= maxFailures && now.Sub(kept[len(kept)-1]) < lockout {
		return loginLocked
	}
	if c.inFlight || s.waiting >= maxWaiting {
		return loginBusy
	}
	c.inFlight = true
	s.waiting++
	return loginOK
}

// done ends addr's running attempt, recording a failure unless ok, or
// clearing its failures if ok.
func (s *sessions) done(addr string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiting--
	c := s.clients[addr]
	if c == nil {
		return
	}
	c.inFlight = false
	if ok {
		c.failures = nil
	} else {
		c.failures = append(c.failures, s.now())
	}
}

// forgetOldestClient drops the address seen longest ago that has no
// attempt running. Called with mu held.
func (s *sessions) forgetOldestClient() {
	oldest := ""
	var when time.Time
	for addr, c := range s.clients {
		if c.inFlight {
			continue
		}
		if oldest == "" || c.last.Before(when) {
			oldest, when = addr, c.last
		}
	}
	delete(s.clients, oldest)
}

// dropOldest removes the session that expires first. Called with mu held.
func (s *sessions) dropOldest() {
	var oldest [32]byte
	first := true
	var when time.Time
	for k, e := range s.active {
		if first || e.Before(when) {
			oldest, when, first = k, e, false
		}
	}
	delete(s.active, oldest)
}
