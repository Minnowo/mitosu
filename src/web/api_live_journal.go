// GET /api/live/journal?host=&unit=&priority=&lines=&before=: the newest
// journal entries, optionally of one unit and at or above a priority.

package web

import (
	"net/http"

	"mitosu/src/live"
)

func (s *Server) handleLiveJournal(w http.ResponseWriter, r *http.Request, host string) {
	lines, ok := intParam(w, r, "lines", 200)
	if !ok {
		return
	}
	priority, ok := intParam(w, r, "priority", -1)
	if !ok {
		return
	}
	before := r.URL.Query().Get("before")
	q := live.JournalQuery{Unit: r.URL.Query().Get("unit"), Priority: int(max(min(priority, 8), -2)), Lines: int(min(lines, live.MaxLines+1)), Before: before}
	res, err := live.Journal(r.Context(), s.opts.Live, s.opts.LiveOptions, host, q)
	liveAnswer(w, host, res, err)
}
