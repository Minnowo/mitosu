// Tests for the pull loop with a fake connection and puller: periodic
// pulls, pull now, retries with pull status events, reconnecting after a
// dead connection, and stopping.

package scheduler

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mitosu/src/pull"
	"mitosu/src/ssh"
	"mitosu/src/store"
)

func TestPullsEveryInterval(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() >= 3 })
	cancel()
	<-done
	st := s.Status()[0]
	if st.LastError != "" || st.LastSuccess.IsZero() || st.LastSamples != 1 {
		t.Fatalf("status %+v", st)
	}
	if f.connects.Load() != 1 {
		t.Fatalf("connected %d times, want the connection kept", f.connects.Load())
	}
}

func TestPullNow(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	if err := s.PullNow("web"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.pulls.Load() == 2 })
	if err := s.PullNow("nope"); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("unknown host: %v", err)
	}
	cancel()
	<-done
}

func TestFailuresAreRetriedAndRecordedOnce(t *testing.T) {
	f := newFake()
	f.failConnects.Store(3)
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	cancel()
	<-done
	events, err := f.db.Events("web", 0, 1<<62, 100000)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if len(kinds) != 2 || kinds[0] != store.EventPullFailed || kinds[1] != store.EventPullOK {
		t.Fatalf("events %v, want one pull_failed then one pull_ok", kinds)
	}
}

func TestDeadConnectionIsReplaced(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() >= 1 })
	f.dead.Store(true)
	waitFor(t, func() bool { return f.connects.Load() >= 2 })
	cancel()
	<-done
}

func TestStopCutsOffARunningPull(t *testing.T) {
	f := newFake()
	f.block.Store(true)
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.blocked.Load() })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return while a pull was running")
	}
}

func TestStopDuringConnect(t *testing.T) {
	f := newFake()
	f.hangConnect.Store(true)
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return while connecting")
	}
}

func TestRetryWait(t *testing.T) {
	first, interval := 30*time.Second, 5*time.Minute
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i, w := range want {
		if got := retryWait(i+1, first, interval); got != w {
			t.Errorf("failure %d: %v, want %v", i+1, got, w)
		}
	}
	if got := retryWait(1_000_000, first, interval); got != interval {
		t.Errorf("after many failures: %v", got)
	}
}

func TestFailingStateSurvivesRestart(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, time.Hour)
	if err := f.db.RecordPullStatus("web", 1, true, "connection refused"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	cancel()
	<-done
	failing, err := f.db.PullFailing("web")
	if err != nil || failing {
		t.Fatalf("still failing after a good pull: %v %v", failing, err)
	}
}

func TestBadPullKeepsALiveConnection(t *testing.T) {
	f := newFake()
	f.failPulls.Store(2)
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	cancel()
	<-done
	if f.connects.Load() != 1 {
		t.Fatalf("connected %d times; a live connection was dropped after pull errors", f.connects.Load())
	}
}

func TestExecSharesTheConnection(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, time.Hour)
	if _, err := s.Exec(context.Background(), "web", "true", time.Second, nil); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("before Run: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	if _, err := s.Exec(ctx, "web", "docker logs", time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exec(ctx, "nope", "true", time.Second, nil); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("unknown host: %v", err)
	}
	cancel()
	<-done
	if f.connects.Load() != 1 || f.execs.Load() != 1 {
		t.Fatalf("connects %d execs %d, want the pull's connection reused", f.connects.Load(), f.execs.Load())
	}
}

// A connection that died under a live view is replaced for the next one,
// not only at the next pull (which may be hours away).
func TestExecReplacesADeadConnection(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	f.dead.Store(true)
	if res, _ := s.Exec(ctx, "web", "docker logs", time.Second, nil); res.Status != -1 {
		t.Fatalf("on a dead connection: %+v", res)
	}
	if res, err := s.Exec(ctx, "web", "docker logs", time.Second, nil); err != nil || res.Status != 0 {
		t.Fatalf("after: %+v %v", res, err)
	}
	cancel()
	<-done
	if f.connects.Load() != 2 {
		t.Fatalf("connected %d times, want a new connection after the dead one", f.connects.Load())
	}
}

// A live command that ended without an exit status on a working
// connection (its output was cut off) neither drops the connection nor
// checks it under the lock.
func TestExecKeepsAConnectionAfterACutOffCommand(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	alives := f.alives.Load()
	f.cutOff.Store(true)
	for i := 0; i < 3; i++ {
		if res, err := s.Exec(ctx, "web", "docker logs", time.Second, nil); err != nil || res.Status != -1 {
			t.Fatalf("cut off: %+v %v", res, err)
		}
	}
	cancel()
	<-done
	if f.connects.Load() != 1 || f.alives.Load() != alives {
		t.Fatalf("connects %d, liveness checks %d, want the connection kept unchecked", f.connects.Load(), f.alives.Load()-alives)
	}
}

func TestLiveCommandsAreLimitedAndStopWithTheirRequest(t *testing.T) {
	f := newFake()
	s := f.scheduler(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := run(ctx, s)
	waitFor(t, func() bool { return f.pulls.Load() == 1 })
	f.blockExec.Store(true)
	reqCtx, stopReqs := context.WithCancel(ctx)
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := s.Exec(reqCtx, "web", "journalctl", time.Hour, nil)
			results <- err
		}()
	}
	waitFor(t, func() bool { return f.running.Load() == maxLive })
	time.Sleep(20 * time.Millisecond)
	if n := f.running.Load(); n != maxLive {
		t.Fatalf("%d live commands running at once, want at most %d", n, maxLive)
	}
	// The browser went away: running commands stop, waiting ones give up.
	stopReqs()
	for i := 0; i < 4; i++ {
		select {
		case <-results:
		case <-time.After(5 * time.Second):
			t.Fatal("a live command did not stop with its request")
		}
	}
	cancel()
	<-done
}

// fake is a host whose connection and pulls the test controls.
type fake struct {
	db           *store.DB
	connects     atomic.Int32
	failConnects atomic.Int32
	pulls        atomic.Int32
	dead         atomic.Bool
	block        atomic.Bool
	blocked      atomic.Bool
	hangConnect  atomic.Bool
	failPulls    atomic.Int32
	execs        atomic.Int32
	blockExec    atomic.Bool
	running      atomic.Int32
	cutOff       atomic.Bool
	alives       atomic.Int32
}

func newFake() *fake {
	return &fake{}
}

func (f *fake) scheduler(t *testing.T, interval time.Duration) *Scheduler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "mitosu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f.db = db
	connect := func(ctx context.Context, host string) (Conn, error) {
		if f.hangConnect.Load() {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if f.failConnects.Load() > 0 {
			f.failConnects.Add(-1)
			return nil, errors.New("connection refused")
		}
		f.connects.Add(1)
		f.dead.Store(false)
		return &fakeConn{f: f, closed: make(chan struct{})}, nil
	}
	pullFn := func(client pull.Runner, db *store.DB, host string, tiers []store.Tier, now func() time.Time) (pull.Result, error) {
		if f.block.Load() {
			c := client.(*fakeConn)
			f.blocked.Store(true)
			<-c.closed
			return pull.Result{}, errors.New("connection closed")
		}
		if f.failPulls.Load() > 0 {
			f.failPulls.Add(-1)
			return pull.Result{}, errors.New("database is locked")
		}
		f.pulls.Add(1)
		return pull.Result{Samples: 1}, nil
	}
	s := New(db, []string{"web"}, store.DefaultTiers, interval, connect, pullFn)
	s.firstRetry = time.Millisecond
	return s
}

type fakeConn struct {
	f      *fake
	once   sync.Once
	closed chan struct{}
}

func (c *fakeConn) Exec(string, time.Duration, func(io.Reader) error) (ssh.ExecResult, error) {
	c.f.execs.Add(1)
	if c.f.dead.Load() {
		// The connection is lost while the command runs.
		c.Close()
		return ssh.ExecResult{Status: -1}, nil
	}
	if c.f.cutOff.Load() {
		return ssh.ExecResult{Status: -1}, nil
	}
	return ssh.ExecResult{}, nil
}

func (c *fakeConn) ExecContext(ctx context.Context, cmd string, timeout time.Duration, read func(io.Reader) error) (ssh.ExecResult, error) {
	if c.f.blockExec.Load() {
		c.f.running.Add(1)
		defer c.f.running.Add(-1)
		select {
		case <-ctx.Done():
		case <-c.closed:
		}
		return ssh.ExecResult{Status: -1}, nil
	}
	return c.Exec(cmd, timeout, read)
}

func (c *fakeConn) Alive(time.Duration) bool {
	c.f.alives.Add(1)
	if c.f.dead.Load() {
		c.Close()
		return false
	}
	return true
}

func (c *fakeConn) Closed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *fakeConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func run(ctx context.Context, s *Scheduler) chan struct{} {
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	return done
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5 s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
