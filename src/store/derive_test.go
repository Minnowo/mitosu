// Tests for turning samples into stored values: gauges, rates, and the
// rules that skip a rate (reboot, reset, recreated container, long gap).

package store

import (
	"math"
	"testing"
	"time"

	"mitosu/src/kanshi"
)

const bootA = "63269f57-5133-4a1a-a9b7-2d6868e7ae87"

func TestGaugesFromOneSample(t *testing.T) {
	s := baseSample(1, 1000)
	got := valueMap(derive(nil, s))
	want := map[string]float64{
		"host//mem_used_bytes":          (8000 - 6000) * 1024,
		"host//mem_total_bytes":         8000 * 1024,
		"host//load1":                   0.33,
		"fs///used_bytes":               600,
		"fs///avail_bytes":              300,
		"container/web/mem_used_bytes":  900,
		"container/web/pids":            7,
		"container/web/mem_limit_bytes": 5000,
	}
	for k, v := range want {
		if math.Abs(got[k]-v) > 1e-9 {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	for k := range got {
		if k == "host//cpu_busy_pct" || k == "net/ens3/rx_bps" || k == "container/web/cpu_cores" {
			t.Errorf("rate %s from a single sample", k)
		}
	}
}

func TestRatesBetweenSamples(t *testing.T) {
	a := baseSample(1, 0)
	b := baseSample(2, 10000) // 10 s later
	b.CPU.Total.User += 300   // of 1000 ticks
	b.CPU.Total.Idle += 600
	b.CPU.Total.Iowait += 100
	b.CPU.Cores[0].User += 50
	b.CPU.Cores[0].Idle += 50
	(*b.Net)[0].RxBytes += 1000
	(*b.Disks)[0].ReadSectors += 20
	(*b.Disks)[0].IoMs += 2500
	(*b.Containers)[0].Stats.CPUNs += 5e9
	got := valueMap(derive(a, b))
	want := map[string]float64{
		"host//cpu_busy_pct":      30,
		"host//cpu_user_pct":      30,
		"host//cpu_iowait_pct":    10,
		"core/0/busy_pct":         50,
		"net/ens3/rx_bps":         100,
		"disk/vda/read_bps":       20 * 512 / 10.0,
		"disk/vda/busy_pct":       25,
		"container/web/cpu_cores": 0.5,
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || math.Abs(g-v) > 1e-9 {
			t.Errorf("%s = %v (present %v), want %v", k, g, ok, v)
		}
	}
}

func TestNoRateAcrossResets(t *testing.T) {
	cases := map[string]func(a, b *kanshi.Sample){
		"reboot":       func(a, b *kanshi.Sample) { other := "11111111-2222-3333-4444-555555555555"; b.BootID = &other },
		"no boot id":   func(a, b *kanshi.Sample) { b.BootID = nil },
		"long gap":     func(a, b *kanshi.Sample) { b.TimeMs = a.TimeMs + uint64((maxRateGap + time.Second).Milliseconds()) },
		"clock back":   func(a, b *kanshi.Sample) { a.TimeMs = 20000; b.TimeMs = 19999 },
		"counter down": func(a, b *kanshi.Sample) { b.CPU.Total.User = 0; (*b.Net)[0].RxBytes = 0 },
	}
	for name, change := range cases {
		a := baseSample(1, 0)
		b := baseSample(2, 10000)
		b.CPU.Total.Idle += 100
		change(a, b)
		got := valueMap(derive(a, b))
		for _, k := range []string{"host//cpu_busy_pct", "net/ens3/rx_bps"} {
			if _, ok := got[k]; ok {
				t.Errorf("%s: %s present", name, k)
			}
		}
		if _, ok := got["host//mem_used_bytes"]; !ok {
			t.Errorf("%s: gauges missing", name)
		}
	}
}

func TestContainerCountersRestart(t *testing.T) {
	for name, change := range map[string]func(c *kanshi.Container){
		"recreated": func(c *kanshi.Container) { c.ID = "b" + c.ID[1:] },
		"restarted": func(c *kanshi.Container) { c.StartedAtMs += 5000 },
	} {
		a := baseSample(1, 0)
		b := baseSample(2, 10000)
		c := &(*b.Containers)[0]
		c.Stats.CPUNs += 1e9
		change(c)
		if _, ok := valueMap(derive(a, b))["container/web/cpu_cores"]; ok {
			t.Errorf("%s: cpu rate across a container restart", name)
		}
	}
}

func TestEntriesThatComeAndGo(t *testing.T) {
	a := baseSample(1, 0)
	b := baseSample(2, 10_000)
	b.CPU.Total.Idle += 100
	b.CPU.Cores = []kanshi.Core{{ID: 1, CPUTimes: b.CPU.Total}} // core 0 offline, 1 new
	(*b.Net)[0].Name = "ens4"                                   // interface renamed
	*b.Disks = append(*b.Disks, kanshi.Disk{Name: "vdb"})       // disk added
	a.Vmstat = nil                                              // section absent before
	(*a.Containers)[0].Stats = nil                              // stats absent before
	got := valueMap(derive(a, b))
	for _, k := range []string{"core/0/busy_pct", "core/1/busy_pct", "net/ens4/rx_bps", "disk/vdb/read_bps", "host//swap_in_pps", "container/web/cpu_cores"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s has a rate without a matching earlier value", k)
		}
	}
	if _, ok := got["host//cpu_busy_pct"]; !ok {
		t.Error("host CPU rate missing")
	}
}

func TestWrapsAndIowait(t *testing.T) {
	a := baseSample(1, 0)
	b := baseSample(2, 10_000)
	(*a.Disks)[0].IoMs = 4294967000 // 32-bit counter about to wrap
	(*b.Disks)[0].IoMs = 500
	(*b.Disks)[0].ReadSectors += 10
	b.CPU.Total.Idle += 1000
	b.CPU.Total.Iowait -= 50 // the kernel may lower iowait
	got := valueMap(derive(a, b))
	if _, ok := got["disk/vda/busy_pct"]; ok {
		t.Error("busy rate across an io_ms wrap")
	}
	if _, ok := got["disk/vda/read_bps"]; !ok {
		t.Error("read rate missing (its counter did not wrap)")
	}
	if v, ok := got["host//cpu_busy_pct"]; !ok || v < 0 || v > 100 {
		t.Errorf("cpu busy %v (present %v) with iowait going down", v, ok)
	}
}

func TestNetErrorCountersResetSeparately(t *testing.T) {
	a := baseSample(1, 0)
	b := baseSample(2, 10_000)
	(*a.Net)[0].RxErrs = 100
	(*b.Net)[0].RxErrs = 0   // reset
	(*b.Net)[0].RxDrop = 500 // grew more than errs fell
	if _, ok := valueMap(derive(a, b))["net/ens3/rx_errors_ps"]; ok {
		t.Fatal("error rate across a reset of one of its counters")
	}
}

func valueMap(values []value) map[string]float64 {
	m := map[string]float64{}
	for _, v := range values {
		m[v.kind+"/"+v.entity+"/"+v.metric] = v.v
	}
	return m
}

// baseSample returns a small sample with every section, at seq and time
// t (milliseconds).
func baseSample(seq, t uint64) *kanshi.Sample {
	boot := bootA
	uptime := uint64(1000)
	cpu := kanshi.CPUTimes{User: 1000, System: 500, Idle: 8000, Iowait: 100}
	return &kanshi.Sample{
		Seq: seq, TimeMs: t, BootID: &boot, UptimeMs: &uptime,
		CPU:    &kanshi.CPU{Total: cpu, Cores: []kanshi.Core{{ID: 0, CPUTimes: cpu}}},
		Mem:    &kanshi.Mem{TotalKiB: 8000, AvailableKiB: 6000, FreeKiB: 5000},
		Load:   &kanshi.Load{Avg1x100: 33, Tasks: 100},
		Vmstat: &kanshi.Vmstat{},
		FS:     &[]kanshi.FS{{Mount: "/", Source: "/dev/vda1", Type: "ext4", SizeBytes: 1000, FreeBytes: 400, AvailBytes: 300}},
		Disks:  &[]kanshi.Disk{{Name: "vda", ReadSectors: 100, IoMs: 1000}},
		Net:    &[]kanshi.NetIf{{Name: "ens3", NetCounters: kanshi.NetCounters{RxBytes: 5000}}},
		Containers: &[]kanshi.Container{{
			Name: "web", ID: "a123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"[:64],
			Image: "nginx", State: "running", StartedAtMs: 500,
			Stats: &kanshi.ContainerStats{CPUNs: 1e9, MemBytes: 1000, MemInactiveFileBytes: 100, MemLimitBytes: 5000, Pids: 7},
		}},
		Errors: &[]kanshi.Problem{},
	}
}
