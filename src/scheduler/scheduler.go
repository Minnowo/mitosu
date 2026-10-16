// Pulls every configured host on a timer for `mitosu serve`: one
// goroutine and one kept SSH connection per host, retries with backoff,
// pull status changes recorded as events, and a "pull now" trigger.

package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"mitosu/src/pull"
	"mitosu/src/store"
)

const (
	// firstRetry is the wait after a first failure; it doubles with each
	// further failure up to the pull interval.
	firstRetry = 30 * time.Second

	// aliveTimeout bounds the check of a kept connection before a pull.
	aliveTimeout = 15 * time.Second
)

// Conn is an open connection to a host; *ssh.SSHClient is one.
type Conn interface {
	pull.Runner
	// Alive reports whether the connection still answers; a dead one is
	// closed.
	Alive(timeout time.Duration) bool
	Close() error
}

// Connector opens the connection to a host, giving up when ctx is done.
// It must not prompt.
type Connector func(ctx context.Context, host string) (Conn, error)

// Puller pulls one host over an open connection (pull.Pull in serve).
type Puller func(client pull.Runner, db *store.DB, host string, tiers []store.Tier, now func() time.Time) (pull.Result, error)

// HostStatus is what the scheduler knows about one host's pulls.
type HostStatus struct {
	Host string
	// Pulling is true while a pull runs.
	Pulling bool
	// LastAttempt and LastSuccess are zero before the first one.
	LastAttempt time.Time
	LastSuccess time.Time
	// LastError is the last pull's error, "" if it worked.
	LastError string
	// LastSamples is how many samples the last successful pull stored.
	LastSamples int
	// NextPull is when the next pull is due.
	NextPull time.Time
}

// Scheduler runs the pulls. Create it with New, then call Run.
type Scheduler struct {
	db       *store.DB
	tiers    []store.Tier
	interval time.Duration
	connect  Connector
	pullFn   Puller
	now      func() time.Time
	// firstRetry is the wait after a first failure (firstRetry const,
	// shorter in tests).
	firstRetry time.Duration

	mu      sync.Mutex
	status  map[string]*HostStatus
	trigger map[string]chan struct{}
	hosts   []string
}

// New returns a scheduler for hosts, pulling each every interval into db
// with tiers.
func New(db *store.DB, hosts []string, tiers []store.Tier, interval time.Duration, connect Connector, pullFn Puller) *Scheduler {
	s := &Scheduler{
		db: db, tiers: tiers, interval: interval, connect: connect, pullFn: pullFn, now: time.Now, firstRetry: firstRetry,
		status: map[string]*HostStatus{}, trigger: map[string]chan struct{}{},
		hosts: append([]string(nil), hosts...),
	}
	for _, h := range hosts {
		s.status[h] = &HostStatus{Host: h}
		s.trigger[h] = make(chan struct{}, 1)
	}
	return s
}

// Run pulls every host until ctx is done, then closes the connections and
// returns once every host's loop has stopped. A pull still running when
// ctx ends is cut off; its page is not stored.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, h := range s.hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			s.runHost(ctx, host)
		}(h)
	}
	wg.Wait()
}

// Status returns every host's status, in config order.
func (s *Scheduler) Status() []HostStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HostStatus, 0, len(s.hosts))
	for _, h := range s.hosts {
		out = append(out, *s.status[h])
	}
	return out
}

// ErrUnknownHost is returned by PullNow for a host not in the config.
var ErrUnknownHost = errors.New("host is not in the config")

// PullNow asks for a pull of host as soon as it is not already pulling.
// It does not wait for the pull.
func (s *Scheduler) PullNow(host string) error {
	ch, ok := s.trigger[host]
	if !ok {
		return ErrUnknownHost
	}
	select {
	case ch <- struct{}{}:
	default:
	}
	return nil
}

// runHost is one host's loop.
func (s *Scheduler) runHost(ctx context.Context, host string) {
	var client Conn
	var clientMu sync.Mutex
	closeClient := func() {
		clientMu.Lock()
		defer clientMu.Unlock()
		if client != nil {
			client.Close()
			client = nil
		}
	}
	// Closing the connection when ctx ends stops a running pull.
	stop := context.AfterFunc(ctx, closeClient)
	defer stop()
	defer closeClient()

	failures := 0
	// Restored from the history, so a restart that finds pulling working
	// again still records it.
	failedBefore, err := s.db.PullFailing(host)
	if err != nil {
		log.Warn().Err(err).Str("host", host).Msg("Reading the last pull status")
	}
	for {
		s.setStatus(host, func(st *HostStatus) {
			st.Pulling = true
			st.LastAttempt = s.now()
		})
		res, err := s.pullOnce(ctx, host, &client, &clientMu)
		if ctx.Err() != nil {
			return
		}
		wait := s.interval
		if err != nil {
			// Keep a connection that still answers: the error may be the
			// database's or kanshi's, not the link's.
			clientMu.Lock()
			if client != nil && !client.Alive(aliveTimeout) {
				client = nil
			}
			clientMu.Unlock()
			failures++
			wait = retryWait(failures, s.firstRetry, s.interval)
			log.Warn().Err(err).Str("host", host).Dur("retry_in", wait).Msg("Pull failed")
			if !failedBefore {
				s.recordStatus(host, true, err.Error())
			}
			failedBefore = true
		} else {
			if failedBefore {
				s.recordStatus(host, false, "")
			}
			failures = 0
			failedBefore = false
			log.Info().Str("host", host).Int("samples", res.Samples).Msg("Pulled")
		}
		s.setStatus(host, func(st *HostStatus) {
			st.Pulling = false
			if err != nil {
				st.LastError = err.Error()
			} else {
				st.LastError = ""
				st.LastSuccess = st.LastAttempt
				st.LastSamples = res.Samples
			}
			st.NextPull = s.now().Add(wait)
		})

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.trigger[host]:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// pullOnce makes sure a live connection exists and pulls once.
func (s *Scheduler) pullOnce(ctx context.Context, host string, client *Conn, mu *sync.Mutex) (pull.Result, error) {
	mu.Lock()
	c := *client
	mu.Unlock()
	if c != nil && !c.Alive(aliveTimeout) {
		log.Info().Str("host", host).Msg("Connection lost; reconnecting")
		c = nil
	}
	if c == nil {
		nc, err := s.connect(ctx, host)
		if err != nil {
			return pull.Result{}, err
		}
		mu.Lock()
		if ctx.Err() != nil {
			mu.Unlock()
			nc.Close()
			return pull.Result{}, ctx.Err()
		}
		*client = nc
		mu.Unlock()
		c = nc
	}
	return s.pullFn(c, s.db, host, s.tiers, s.now)
}

func (s *Scheduler) setStatus(host string, change func(*HostStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s.status[host])
}

// recordStatus stores a pull status change; a failure to store it is
// logged, not fatal.
func (s *Scheduler) recordStatus(host string, failed bool, detail string) {
	if err := s.db.RecordPullStatus(host, s.now().UnixMilli(), failed, detail); err != nil {
		log.Warn().Err(err).Str("host", host).Msg("Recording pull status")
	}
}

// retryWait is the wait after the given number of failures in a row
// (1 or more): first, doubling with each failure, at most interval.
func retryWait(failures int, first, interval time.Duration) time.Duration {
	wait := first
	for i := 1; i < failures && wait < interval; i++ {
		wait *= 2
	}
	return min(wait, interval)
}
