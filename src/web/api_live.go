// What the live view endpoints (/api/live/*) share: the on/off switch,
// parameters, and turning errors into HTTP answers. The views run fixed
// commands over the host's connection (src/live); nothing they return is
// stored or logged.

package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"

	"mitosu/src/live"
	"mitosu/src/scheduler"
)

// liveHandler wraps a live view: 404 when live views are off, else the
// host parameter checked and handed on.
func (s *Server) liveHandler(view func(w http.ResponseWriter, r *http.Request, host string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.opts.LiveViews || s.opts.Live == nil {
			writeError(w, http.StatusNotFound, "live views are off (live_views in mitosu.conf)")
			return
		}
		host, ok := s.hostParam(w, r)
		if !ok {
			return
		}
		view(w, r, host)
	}
}

// liveAnswer writes a live view's result, or its error: 400 for refused
// input, 503 while serve is starting or stopping, 502 for a command that failed on
// the server (the message says why).
func liveAnswer(w http.ResponseWriter, host string, result any, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, live.ErrBadInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, scheduler.ErrNotRunning):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		log.Info().Err(err).Str("host", host).Msg("Live view failed")
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// intParam reads an optional integer query parameter; def if absent, and
// false (after writing 400) if not a number.
func intParam(w http.ResponseWriter, r *http.Request, name string, def int64) (int64, bool) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, name+" must be a whole number")
		return 0, false
	}
	return n, true
}

// knownContainer reports whether host's samples named container, writing
// 404 if not: live views only run for containers kanshi reported.
func (s *Server) knownContainer(w http.ResponseWriter, host, container string) bool {
	containers, err := s.db.Containers(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading containers failed")
		return false
	}
	for _, c := range containers {
		if c.Name == container {
			return true
		}
	}
	writeError(w, http.StatusNotFound, "no such container on this host")
	return false
}
