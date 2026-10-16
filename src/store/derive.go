// Turns kanshi samples into the values mitosu stores: gauges from one
// sample, rates from the counters of two consecutive samples
// (../kanshi/docs/protocol.md, "sample"). A rate is only computed when
// both samples are from the same boot, the time between them is positive
// and at most maxRateGap, and no counter in it went down (a reset).
// Missing values are left out, never stored as zero.

package store

import (
	"strconv"
	"time"

	"mitosu/src/kanshi"
)

// maxRateGap is the longest time between two samples that still gives a
// rate; across a longer gap an average would hide what happened.
const maxRateGap = 15 * time.Minute

// Kinds of series.
const (
	KindHost      = "host"
	KindCore      = "core"
	KindFS        = "fs"
	KindDisk      = "disk"
	KindNet       = "net"
	KindContainer = "container"
)

// value is one stored number of one series at one sample.
type value struct {
	kind, entity, metric string
	v                    float64
}

// derive returns the values for cur. prev is the sample before it from
// the same store, or nil.
func derive(prev, cur *kanshi.Sample) []value {
	var out []value
	add := func(kind, entity, metric string, v float64) {
		out = append(out, value{kind: kind, entity: entity, metric: metric, v: v})
	}

	dt := 0.0
	if prev != nil && cur.TimeMs > prev.TimeMs && cur.TimeMs-prev.TimeMs <= uint64(maxRateGap.Milliseconds()) {
		dt = float64(cur.TimeMs-prev.TimeMs) / 1000
	}
	sameBoot := dt > 0 && prev.BootID != nil && cur.BootID != nil && *prev.BootID == *cur.BootID

	if cur.Mem != nil {
		m := cur.Mem
		add(KindHost, "", "mem_total_bytes", float64(m.TotalKiB)*1024)
		add(KindHost, "", "mem_available_bytes", float64(m.AvailableKiB)*1024)
		if m.TotalKiB >= m.AvailableKiB {
			add(KindHost, "", "mem_used_bytes", float64(m.TotalKiB-m.AvailableKiB)*1024)
		}
		add(KindHost, "", "mem_cache_bytes", float64(m.BuffersKiB+m.CachedKiB)*1024)
		add(KindHost, "", "swap_total_bytes", float64(m.SwapTotalKiB)*1024)
		if m.SwapTotalKiB >= m.SwapFreeKiB {
			add(KindHost, "", "swap_used_bytes", float64(m.SwapTotalKiB-m.SwapFreeKiB)*1024)
		}
	}
	if cur.Load != nil {
		add(KindHost, "", "load1", float64(cur.Load.Avg1x100)/100)
		add(KindHost, "", "load5", float64(cur.Load.Avg5x100)/100)
		add(KindHost, "", "load15", float64(cur.Load.Avg15x100)/100)
		add(KindHost, "", "tasks", float64(cur.Load.Tasks))
	}
	if sameBoot && prev.CPU != nil && cur.CPU != nil {
		if shares, ok := cpuShares(prev.CPU.Total, cur.CPU.Total); ok {
			add(KindHost, "", "cpu_busy_pct", shares.busy)
			add(KindHost, "", "cpu_user_pct", shares.user)
			add(KindHost, "", "cpu_system_pct", shares.system)
			add(KindHost, "", "cpu_iowait_pct", shares.iowait)
			add(KindHost, "", "cpu_steal_pct", shares.steal)
		}
		prevCores := map[uint64]kanshi.CPUTimes{}
		for _, c := range prev.CPU.Cores {
			prevCores[c.ID] = c.CPUTimes
		}
		for _, c := range cur.CPU.Cores {
			p, found := prevCores[c.ID]
			if !found {
				continue
			}
			if shares, ok := cpuShares(p, c.CPUTimes); ok {
				add(KindCore, strconv.FormatUint(c.ID, 10), "busy_pct", shares.busy)
			}
		}
	}
	if sameBoot && prev.Vmstat != nil && cur.Vmstat != nil {
		if r, ok := rate(prev.Vmstat.Pswpin, cur.Vmstat.Pswpin, dt); ok {
			add(KindHost, "", "swap_in_pps", r)
		}
		if r, ok := rate(prev.Vmstat.Pswpout, cur.Vmstat.Pswpout, dt); ok {
			add(KindHost, "", "swap_out_pps", r)
		}
	}

	if cur.FS != nil {
		for _, f := range *cur.FS {
			add(KindFS, f.Mount, "size_bytes", float64(f.SizeBytes))
			add(KindFS, f.Mount, "avail_bytes", float64(f.AvailBytes))
			if f.SizeBytes >= f.FreeBytes {
				add(KindFS, f.Mount, "used_bytes", float64(f.SizeBytes-f.FreeBytes))
			}
			add(KindFS, f.Mount, "inodes", float64(f.Inodes))
			if f.Inodes >= f.InodesFree {
				add(KindFS, f.Mount, "inodes_used", float64(f.Inodes-f.InodesFree))
			}
		}
	}

	if sameBoot && prev.Disks != nil && cur.Disks != nil {
		prevDisks := map[string]kanshi.Disk{}
		for _, d := range *prev.Disks {
			prevDisks[d.Name] = d
		}
		for _, d := range *cur.Disks {
			p, found := prevDisks[d.Name]
			if !found {
				continue
			}
			if r, ok := rate(p.ReadSectors, d.ReadSectors, dt); ok {
				add(KindDisk, d.Name, "read_bps", r*512)
			}
			if r, ok := rate(p.WriteSectors, d.WriteSectors, dt); ok {
				add(KindDisk, d.Name, "write_bps", r*512)
			}
			if r, ok := rate(p.Reads, d.Reads, dt); ok {
				add(KindDisk, d.Name, "read_iops", r)
			}
			if r, ok := rate(p.Writes, d.Writes, dt); ok {
				add(KindDisk, d.Name, "write_iops", r)
			}
			if r, ok := rate(p.IoMs, d.IoMs, dt); ok {
				add(KindDisk, d.Name, "busy_pct", clampPct(r/10))
			}
		}
	}

	if sameBoot && prev.Net != nil && cur.Net != nil {
		prevNet := map[string]kanshi.NetCounters{}
		for _, n := range *prev.Net {
			prevNet[n.Name] = n.NetCounters
		}
		for _, n := range *cur.Net {
			p, found := prevNet[n.Name]
			if !found {
				continue
			}
			addNetRates(add, KindNet, n.Name, "", p, n.NetCounters, dt)
		}
	}

	if cur.Containers != nil {
		prevContainers := map[string]kanshi.Container{}
		if prev != nil && prev.Containers != nil {
			for _, c := range *prev.Containers {
				prevContainers[c.Name] = c
			}
		}
		for _, c := range *cur.Containers {
			if c.Stats == nil {
				continue
			}
			st := c.Stats
			if st.MemBytes >= st.MemInactiveFileBytes {
				add(KindContainer, c.Name, "mem_used_bytes", float64(st.MemBytes-st.MemInactiveFileBytes))
			}
			add(KindContainer, c.Name, "mem_limit_bytes", float64(st.MemLimitBytes))
			add(KindContainer, c.Name, "pids", float64(st.Pids))

			p, found := prevContainers[c.Name]
			// A new ID (recreated) or start time (restarted) restarts the
			// counters, even if they happen to have grown.
			if dt == 0 || !found || p.Stats == nil || p.ID != c.ID || p.StartedAtMs != c.StartedAtMs {
				continue
			}
			ps := p.Stats
			if r, ok := rate(ps.CPUNs, st.CPUNs, dt); ok {
				add(KindContainer, c.Name, "cpu_cores", r/1e9)
			}
			if r, ok := rate(ps.BlkReadBytes, st.BlkReadBytes, dt); ok {
				add(KindContainer, c.Name, "blk_read_bps", r)
			}
			if r, ok := rate(ps.BlkWriteBytes, st.BlkWriteBytes, dt); ok {
				add(KindContainer, c.Name, "blk_write_bps", r)
			}
			if ps.Net != nil && st.Net != nil {
				addNetRates(add, KindContainer, c.Name, "net_", *ps.Net, *st.Net, dt)
			}
		}
	}
	return out
}

// addNetRates adds byte and error rates of a network counter pair, with
// metric names prefixed by prefix.
func addNetRates(add func(kind, entity, metric string, v float64), kind, entity, prefix string, p, c kanshi.NetCounters, dt float64) {
	if r, ok := rate(p.RxBytes, c.RxBytes, dt); ok {
		add(kind, entity, prefix+"rx_bps", r)
	}
	if r, ok := rate(p.TxBytes, c.TxBytes, dt); ok {
		add(kind, entity, prefix+"tx_bps", r)
	}
	// Errors and drops are separate counters: each is checked for a reset.
	if errs, ok := rate(p.RxErrs, c.RxErrs, dt); ok {
		if drops, ok := rate(p.RxDrop, c.RxDrop, dt); ok {
			add(kind, entity, prefix+"rx_errors_ps", errs+drops)
		}
	}
	if errs, ok := rate(p.TxErrs, c.TxErrs, dt); ok {
		if drops, ok := rate(p.TxDrop, c.TxDrop, dt); ok {
			add(kind, entity, prefix+"tx_errors_ps", errs+drops)
		}
	}
}

// rate returns (cur-prev)/dt, or false if the counter went down (reset or
// wrap) or dt is not positive.
func rate(prev, cur uint64, dt float64) (float64, bool) {
	if cur < prev || dt <= 0 {
		return 0, false
	}
	return float64(cur-prev) / dt, true
}

// cpuPcts are shares of CPU time between two samples, in percent.
type cpuPcts struct {
	busy, user, system, iowait, steal float64
}

// cpuShares computes CPU shares from two counter sets, using the
// protocol's busy formula. It returns false if no time passed or a
// counter other than iowait (which the kernel may lower) went down.
func cpuShares(a, b kanshi.CPUTimes) (cpuPcts, bool) {
	if b.User < a.User || b.Nice < a.Nice || b.System < a.System || b.Idle < a.Idle ||
		b.Irq < a.Irq || b.Softirq < a.Softirq || b.Steal < a.Steal {
		return cpuPcts{}, false
	}
	sum := func(t kanshi.CPUTimes) float64 {
		return float64(t.User) + float64(t.Nice) + float64(t.System) + float64(t.Idle) +
			float64(t.Iowait) + float64(t.Irq) + float64(t.Softirq) + float64(t.Steal)
	}
	total := sum(b) - sum(a)
	if total <= 0 {
		return cpuPcts{}, false
	}
	d := func(x, y uint64) float64 { return float64(y) - float64(x) }
	idle := d(a.Idle, b.Idle) + d(a.Iowait, b.Iowait)
	return cpuPcts{
		busy:   clampPct(100 * (1 - idle/total)),
		user:   clampPct(100 * (d(a.User, b.User) + d(a.Nice, b.Nice)) / total),
		system: clampPct(100 * (d(a.System, b.System) + d(a.Irq, b.Irq) + d(a.Softirq, b.Softirq)) / total),
		iowait: clampPct(100 * d(a.Iowait, b.Iowait) / total),
		steal:  clampPct(100 * d(a.Steal, b.Steal) / total),
	}, true
}

// clampPct limits a percentage to 0..100.
func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
