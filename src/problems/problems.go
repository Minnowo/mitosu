// Problems mitosu derives from a host's history for the UI (docs/brief.md
// section 3, "Alerts": indicators only, no notifications): disk, inodes,
// memory, swap, CPU and load high; containers unhealthy, restarting,
// OOM-killed or failed; no new samples; pulls failing; problems kanshi
// itself reported.

package problems

import (
	"fmt"
	"sort"
	"time"

	"mitosu/src/store"
)

// Severities, most severe first.
const (
	Critical = "critical"
	Warning  = "warning"
)

// staleAfter is how much later than one pull interval the newest sample
// may be before the host counts as having no new samples: at least this,
// and at least staleSamples of kanshi's own sampling intervals.
const (
	staleAfter   = 3 * time.Minute
	staleSamples = 3
)

// stopExitCodes are what `docker stop` leaves (SIGTERM, or SIGKILL after
// its timeout): a stopped container is not a failed one.
var stopExitCodes = map[int64]bool{137: true, 143: true}

// Problem is one thing worth looking at.
type Problem struct {
	Severity string `json:"severity"`
	// Subject is what it concerns: "host", a mount point, a container
	// name, "kanshi" or "mitosu".
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// Input is what is known about one host.
type Input struct {
	Now          time.Time
	PullInterval time.Duration
	// LastSampleMs is the newest stored sample's time, 0 if none.
	LastSampleMs int64
	// SampleInterval is kanshi's sampling interval (0 if unknown).
	SampleInterval time.Duration
	// LastError is the last pull's error, "" if it worked.
	LastError string
	// Latest are the newest values (store.DB.Latest), from around the
	// newest sample.
	Latest []store.LatestValue
	// CPUBusy15m is the average CPU busy percent over the 15 minutes up
	// to the newest sample, nil if unknown.
	CPUBusy15m *float64
	Containers []store.ContainerState
	// SampleProblems are the problems the newest sample reported.
	SampleProblems []string
}

// Find returns the problems in in, critical ones first, then by subject.
func Find(in Input) []Problem {
	var out []Problem
	add := func(sev, subject, format string, args ...any) {
		out = append(out, Problem{Severity: sev, Subject: subject, Message: fmt.Sprintf(format, args...)})
	}

	if in.LastError != "" {
		add(Warning, "mitosu", "pulling fails: %s", in.LastError)
	}
	if in.LastSampleMs == 0 {
		add(Warning, "kanshi", "no samples yet")
	} else if age := in.Now.Sub(time.UnixMilli(in.LastSampleMs)); age > in.PullInterval+max(staleAfter, staleSamples*in.SampleInterval) {
		add(Warning, "kanshi", "no new samples for %s", age.Round(time.Minute))
	}
	for _, p := range in.SampleProblems {
		add(Warning, "kanshi", "%s", p)
	}

	v := map[string]float64{}
	cores := 0
	for _, l := range in.Latest {
		v[l.Kind+"|"+l.Entity+"|"+l.Metric] = l.V
		if l.Kind == store.KindCore && l.Metric == "busy_pct" {
			cores++
		}
	}
	get := func(kind, entity, metric string) (float64, bool) {
		x, ok := v[kind+"|"+entity+"|"+metric]
		return x, ok
	}

	for _, l := range in.Latest {
		if l.Kind != store.KindFS || l.Metric != "size_bytes" || l.V <= 0 {
			continue
		}
		mount := l.Entity
		if avail, ok := get(store.KindFS, mount, "avail_bytes"); ok {
			switch free := 100 * avail / l.V; {
			case free < 5:
				add(Critical, mount, "disk %.0f%% free", free)
			case free < 10:
				add(Warning, mount, "disk %.0f%% free", free)
			}
		}
		inodes, ok1 := get(store.KindFS, mount, "inodes")
		used, ok2 := get(store.KindFS, mount, "inodes_used")
		if ok1 && ok2 && inodes > 0 {
			switch pct := 100 * used / inodes; {
			case pct > 95:
				add(Critical, mount, "inodes %.0f%% used", pct)
			case pct > 90:
				add(Warning, mount, "inodes %.0f%% used", pct)
			}
		}
	}

	if total, ok := get(store.KindHost, "", "mem_total_bytes"); ok && total > 0 {
		if avail, ok := get(store.KindHost, "", "mem_available_bytes"); ok {
			switch free := 100 * avail / total; {
			case free < 5:
				add(Critical, "host", "memory %.0f%% available", free)
			case free < 10:
				add(Warning, "host", "memory %.0f%% available", free)
			}
		}
	}
	if total, ok := get(store.KindHost, "", "swap_total_bytes"); ok && total > 0 {
		if used, ok := get(store.KindHost, "", "swap_used_bytes"); ok && 100*used/total > 80 {
			add(Warning, "host", "swap %.0f%% used", 100*used/total)
		}
	}
	if in.CPUBusy15m != nil {
		switch busy := *in.CPUBusy15m; {
		case busy > 95:
			add(Critical, "host", "CPU %.0f%% busy over 15 minutes", busy)
		case busy > 90:
			add(Warning, "host", "CPU %.0f%% busy over 15 minutes", busy)
		}
	}
	if load, ok := get(store.KindHost, "", "load5"); ok && cores > 0 && load > 2*float64(cores) {
		add(Warning, "host", "load %.1f on %d cores", load, cores)
	}

	for _, c := range in.Containers {
		switch {
		case c.State == "dead":
			add(Critical, c.Name, "container is dead")
		case c.Health == "unhealthy":
			add(Critical, c.Name, "container is unhealthy")
		case c.State == "restarting":
			add(Warning, c.Name, "container is restarting (%d restarts)", c.Restarts)
		case c.State == "exited" && c.ExitCode != 0 && !stopExitCodes[c.ExitCode]:
			add(Warning, c.Name, "container exited with code %d", c.ExitCode)
		}
		if c.OOMKilled {
			add(Warning, c.Name, "container was killed for using too much memory")
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == Critical
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}
