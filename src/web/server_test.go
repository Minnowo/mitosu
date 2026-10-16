// Tests for the web server's API through its real handler: request
// guard, login and its rate limit, and each endpoint.

package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mitosu/src/auth"
	"mitosu/src/kanshi"
	"mitosu/src/pull"
	"mitosu/src/scheduler"
	"mitosu/src/store"
)

const password = "correct horse"

func TestHostHeader(t *testing.T) {
	h := newTestServer(t, false).Handler()
	for host, want := range map[string]int{
		"127.0.0.1:7878":    http.StatusOK,
		"localhost:7878":    http.StatusOK,
		"[::1]:7878":        http.StatusOK,
		"192.168.1.5":       http.StatusOK,
		"mitosu.lan":        http.StatusOK,
		"MITOSU.LAN.:7878":  http.StatusOK,
		"evil.example.com":  http.StatusMisdirectedRequest,
		"127.0.0.1.evil.io": http.StatusMisdirectedRequest,
		"":                  http.StatusMisdirectedRequest,
	} {
		r := apiRequest("GET", "/api/session", "")
		r.Host = host
		if got := serve(h, r).Code; got != want {
			t.Errorf("Host %q: %d, want %d", host, got, want)
		}
	}
}

func TestAPIHeaderAndSecurityHeaders(t *testing.T) {
	h := newTestServer(t, false).Handler()
	r := apiRequest("GET", "/api/hosts", "")
	r.Header.Del(apiHeader)
	if got := serve(h, r).Code; got != http.StatusForbidden {
		t.Fatalf("without %s: %d", apiHeader, got)
	}
	w := serve(h, apiRequest("GET", "/api/hosts", ""))
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if w.Header().Get(header) == "" {
			t.Errorf("%s missing", header)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("API response may be cached")
	}
}

func TestLogin(t *testing.T) {
	h := newTestServer(t, true).Handler()
	if got := serve(h, apiRequest("GET", "/api/hosts", "")).Code; got != http.StatusUnauthorized {
		t.Fatalf("hosts before login: %d", got)
	}
	var session map[string]bool
	decode(t, serve(h, apiRequest("GET", "/api/session", "")), &session)
	if !session["login"] || session["logged_in"] {
		t.Fatalf("session before login %v", session)
	}
	if got := serve(h, apiRequest("POST", "/api/login", `{"password":"wrong password"}`)).Code; got != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", got)
	}
	w := serve(h, apiRequest("POST", "/api/login", `{"password":"`+password+`"}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("right password: %d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if cookie.Name != cookieName || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || len(cookie.Value) < 40 {
		t.Fatalf("cookie %+v", cookie)
	}
	r := apiRequest("GET", "/api/hosts", "")
	r.AddCookie(cookie)
	if got := serve(h, r).Code; got != http.StatusOK {
		t.Fatalf("hosts after login: %d", got)
	}
	r = apiRequest("POST", "/api/logout", "")
	r.AddCookie(cookie)
	serve(h, r)
	r = apiRequest("GET", "/api/hosts", "")
	r.AddCookie(cookie)
	if got := serve(h, r).Code; got != http.StatusUnauthorized {
		t.Fatalf("hosts after logout: %d", got)
	}
	for _, ep := range []struct{ method, path string }{
		{"GET", "/api/hosts"}, {"GET", "/api/series?host=web"},
		{"GET", "/api/points?host=web&kind=host&metric=mem_used_bytes&from=0&to=60000"},
		{"GET", "/api/events?host=web&from=0&to=60000"}, {"POST", "/api/pull?host=web"},
	} {
		if got := serve(h, apiRequest(ep.method, ep.path, "")).Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", ep.method, ep.path, got)
		}
	}
	r = apiRequest("GET", "/api/hosts", "")
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "forged"})
	if got := serve(h, r).Code; got != http.StatusUnauthorized {
		t.Fatalf("forged cookie: %d", got)
	}
}

func TestLoginLockout(t *testing.T) {
	s := newTestServer(t, true)
	now := time.Unix(1_700_000_000, 0)
	s.sessions.now = func() time.Time { return now }
	h := s.Handler()
	for i := 0; i < maxFailures; i++ {
		serve(h, apiRequest("POST", "/api/login", `{"password":"wrong password"}`))
	}
	if got := serve(h, apiRequest("POST", "/api/login", `{"password":"`+password+`"}`)).Code; got != http.StatusTooManyRequests {
		t.Fatalf("right password during lockout: %d", got)
	}
	now = now.Add(lockout + time.Second)
	if got := serve(h, apiRequest("POST", "/api/login", `{"password":"`+password+`"}`)).Code; got != http.StatusNoContent {
		t.Fatalf("after lockout: %d", got)
	}
}

func TestParallelLoginsDoNotPassTheLimit(t *testing.T) {
	h := newTestServer(t, true).Handler()
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code := serve(h, apiRequest("POST", "/api/login", `{"password":"wrong password"}`)).Code
			mu.Lock()
			codes[code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if codes[http.StatusUnauthorized] > maxFailures || codes[http.StatusUnauthorized]+codes[http.StatusTooManyRequests] != 30 {
		t.Fatalf("answers %v: more than %d passwords were checked", codes, maxFailures)
	}
}

func TestOneAddressCannotLockOutAnother(t *testing.T) {
	s := newTestServer(t, true)
	now := time.Unix(1_700_000_000, 0)
	s.sessions.now = func() time.Time { return now }
	h := s.Handler()
	for i := 0; i < 3*maxFailures; i++ {
		r := apiRequest("POST", "/api/login", `{"password":"wrong password"}`)
		r.RemoteAddr = "203.0.113.9:4000"
		serve(h, r)
		now = now.Add(time.Second)
	}
	r := apiRequest("POST", "/api/login", `{"password":"`+password+`"}`)
	r.RemoteAddr = "198.51.100.7:5000"
	if got := serve(h, r).Code; got != http.StatusNoContent {
		t.Fatalf("owner's login while another address is locked out: %d", got)
	}
}

func TestLoginOff(t *testing.T) {
	h := newTestServer(t, false).Handler()
	if got := serve(h, apiRequest("GET", "/api/hosts", "")).Code; got != http.StatusOK {
		t.Fatalf("hosts with login off: %d", got)
	}
}

func TestBadRequests(t *testing.T) {
	h := newTestServer(t, false).Handler()
	cases := map[string]int{
		"/api/points?host=web&kind=host&metric=mem_used_bytes&from=10&to=5":      http.StatusBadRequest,
		"/api/points?host=web&kind=host&metric=mem_used_bytes&from=0&to=9&max=0": http.StatusBadRequest,
		"/api/points?host=web&kind=host&metric=nope&from=0&to=9":                 http.StatusNotFound,
		"/api/points?host=other&kind=host&metric=mem_used_bytes&from=0&to=9":     http.StatusNotFound,
		"/api/series?host=": http.StatusNotFound,
		"/api/nope":         http.StatusNotFound,
	}
	for path, want := range cases {
		if got := serve(h, apiRequest("GET", path, "")).Code; got != want {
			t.Errorf("%s: %d, want %d", path, got, want)
		}
	}
	if got := serve(h, apiRequest("DELETE", "/api/hosts", "")).Code; got != http.StatusNotFound {
		t.Errorf("DELETE /api/hosts: %d", got)
	}
	big := `{"password":"` + strings.Repeat("x", maxBody) + `"}`
	if got := serve(newTestServer(t, true).Handler(), apiRequest("POST", "/api/login", big)).Code; got != http.StatusBadRequest {
		t.Errorf("oversized login body: %d", got)
	}
}

func TestEndpoints(t *testing.T) {
	s := newTestServer(t, false)
	h := s.Handler()

	var hosts []hostJSON
	decode(t, serve(h, apiRequest("GET", "/api/hosts", "")), &hosts)
	if len(hosts) != 1 || hosts[0].Name != "web" || hosts[0].NextSeq != 2 || hosts[0].LastSampleMs != 20_000 {
		t.Fatalf("hosts %+v", hosts)
	}
	var series []seriesJSON
	decode(t, serve(h, apiRequest("GET", "/api/series?host=web", "")), &series)
	if len(series) == 0 {
		t.Fatal("no series")
	}
	var points pointsJSON
	decode(t, serve(h, apiRequest("GET", "/api/points?host=web&kind=host&entity=&metric=mem_used_bytes&from=0&to=60000", "")), &points)
	if points.Resolution != 0 || len(points.Points) != 2 || points.Points[1][0] != 20_000 || points.Points[1][1] != 2000*1024 {
		t.Fatalf("points %+v", points)
	}
	var events []eventJSON
	decode(t, serve(h, apiRequest("GET", "/api/events?host=web&from=0&to=60000", "")), &events)
	if events == nil {
		t.Fatal("events not a list")
	}
	if got := serve(h, apiRequest("POST", "/api/pull?host=web", "")).Code; got != http.StatusAccepted {
		t.Fatalf("pull now: %d", got)
	}
}

// newTestServer returns a server for host "web" holding two samples; with
// login, the password is `password`.
func newTestServer(t *testing.T, login bool) *Server {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mitosu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	page := store.Page{Header: kanshi.Header{Kanshi: "1.0.0", Format: "1.0", Store: "0123456789abcdef0123456789abcdef"}, End: kanshi.End{Next: 2}}
	for i, ts := range []uint64{10_000, 20_000} {
		line := []byte(`{"type":"sample","seq":` + strconv.Itoa(i+1) + `,"time_ms":` + strconv.FormatUint(ts, 10) + `,"mem":{"total_kib":8000,"free_kib":5000,"available_kib":6000,"buffers_kib":0,"cached_kib":0,"swap_total_kib":0,"swap_free_kib":0},"errors":[]}`)
		var s kanshi.Sample
		if err := json.Unmarshal(line, &s); err != nil {
			t.Fatal(err)
		}
		page.Samples = append(page.Samples, store.PulledSample{Sample: s, Line: line})
	}
	if _, err := db.Ingest("web", store.DefaultTiers, page); err != nil {
		t.Fatal(err)
	}
	sched := scheduler.New(db, []string{"web"}, store.DefaultTiers, time.Hour,
		func(context.Context, string) (scheduler.Conn, error) { return nil, io.EOF },
		func(pull.Runner, *store.DB, string, []store.Tier, func() time.Time) (pull.Result, error) {
			return pull.Result{}, nil
		})
	opts := Options{Listen: "127.0.0.1:7878", AllowedHosts: []string{"mitosu.lan"}, Tiers: store.DefaultTiers}
	if login {
		hash, err := auth.Hash(password)
		if err != nil {
			t.Fatal(err)
		}
		v, err := auth.ParseHash(hash)
		if err != nil {
			t.Fatal(err)
		}
		opts.Verifier = v
	}
	s := New(db, sched, opts)
	s.now = func() time.Time { return time.UnixMilli(30_000) }
	return s
}

func apiRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:7878"
	r.Header.Set(apiHeader, "1")
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
}
