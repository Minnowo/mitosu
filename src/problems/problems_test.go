// Tests for the problem rules: each threshold on both sides, stale data,
// failing pulls and container states.

package problems

import (
	"strings"
	"testing"
	"time"

	"mitosu/src/store"
)

var now = time.UnixMilli(1_700_000_000_000)

func TestHealthyHostHasNoProblems(t *testing.T) {
	if got := Find(healthy()); len(got) != 0 {
		t.Fatalf("problems on a healthy host: %+v", got)
	}
}

func TestThresholds(t *testing.T) {
	cases := []struct {
		name   string
		change func(in *Input)
		want   string // "severity subject message-part", "" for none
	}{
		{"disk 9% free", func(in *Input) { set(in, "fs", "/", "avail_bytes", 9) }, "warning / disk 9% free"},
		{"disk 4% free", func(in *Input) { set(in, "fs", "/", "avail_bytes", 4) }, "critical / disk 4% free"},
		{"disk 10% free", func(in *Input) { set(in, "fs", "/", "avail_bytes", 10) }, ""},
		{"inodes 91%", func(in *Input) { set(in, "fs", "/", "inodes_used", 91) }, "warning / inodes 91% used"},
		{"inodes 96%", func(in *Input) { set(in, "fs", "/", "inodes_used", 96) }, "critical / inodes 96% used"},
		{"memory 8%", func(in *Input) { set(in, "host", "", "mem_available_bytes", 8) }, "warning host memory 8% available"},
		{"memory 3%", func(in *Input) { set(in, "host", "", "mem_available_bytes", 3) }, "critical host memory 3% available"},
		{"swap 85%", func(in *Input) { set(in, "host", "", "swap_used_bytes", 85) }, "warning host swap 85% used"},
		{"cpu 93%", func(in *Input) { x := 93.0; in.CPUBusy15m = &x }, "warning host CPU 93% busy"},
		{"cpu 99%", func(in *Input) { x := 99.0; in.CPUBusy15m = &x }, "critical host CPU 99% busy"},
		{"load 9 on 4", func(in *Input) { set(in, "host", "", "load5", 9) }, "warning host load 9.0 on 4 cores"},
		{"load 8 on 4", func(in *Input) { set(in, "host", "", "load5", 8) }, ""},
		{"stale", func(in *Input) { in.LastSampleMs = now.Add(-9 * time.Minute).UnixMilli() }, "warning kanshi no new samples for 9m"},
		{"no samples", func(in *Input) { in.LastSampleMs = 0 }, "warning kanshi no samples yet"},
		{"pull error", func(in *Input) { in.LastError = "connection refused" }, "warning mitosu pulling fails: connection refused"},
		{"kanshi problem", func(in *Input) { in.SampleProblems = []string{"fs: /boot/efi: statvfs failed"} }, "warning kanshi fs: /boot/efi"},
		{"unhealthy", func(in *Input) { in.Containers[0].Health = "unhealthy" }, "critical web container is unhealthy"},
		{"restarting", func(in *Input) { in.Containers[0].State = "restarting"; in.Containers[0].Restarts = 4 }, "warning web container is restarting (4 restarts)"},
		{"exited 1", func(in *Input) { in.Containers[0].State = "exited"; in.Containers[0].ExitCode = 1 }, "warning web container exited with code 1"},
		{"exited 0", func(in *Input) { in.Containers[0].State = "exited" }, ""},
		{"stopped 143", func(in *Input) { in.Containers[0].State = "exited"; in.Containers[0].ExitCode = 143 }, ""},
		{"stopped 137", func(in *Input) { in.Containers[0].State = "exited"; in.Containers[0].ExitCode = 137 }, ""},
		{"slow kanshi not stale", func(in *Input) {
			in.SampleInterval = 10 * time.Minute
			in.LastSampleMs = now.Add(-30 * time.Minute).UnixMilli()
		}, ""},
		{"slow kanshi stale", func(in *Input) {
			in.SampleInterval = 10 * time.Minute
			in.LastSampleMs = now.Add(-36 * time.Minute).UnixMilli()
		}, "warning kanshi no new samples for 36m"},
		{"stale boundary", func(in *Input) { in.LastSampleMs = now.Add(-8 * time.Minute).UnixMilli() }, ""},
		{"disk 10% free exactly", func(in *Input) { set(in, "fs", "/", "avail_bytes", 10) }, ""},
		{"disk 5% free exactly", func(in *Input) { set(in, "fs", "/", "avail_bytes", 5) }, "warning / disk 5% free"},
		{"inodes 90% exactly", func(in *Input) { set(in, "fs", "/", "inodes_used", 90) }, ""},
		{"inodes 95% exactly", func(in *Input) { set(in, "fs", "/", "inodes_used", 95) }, "warning / inodes 95% used"},
		{"memory 10% exactly", func(in *Input) { set(in, "host", "", "mem_available_bytes", 10) }, ""},
		{"memory 5% exactly", func(in *Input) { set(in, "host", "", "mem_available_bytes", 5) }, "warning host memory 5% available"},
		{"swap 80% exactly", func(in *Input) { set(in, "host", "", "swap_used_bytes", 80) }, ""},
		{"swap 81%", func(in *Input) { set(in, "host", "", "swap_used_bytes", 81) }, "warning host swap 81% used"},
		{"cpu 90% exactly", func(in *Input) { x := 90.0; in.CPUBusy15m = &x }, ""},
		{"cpu 95% exactly", func(in *Input) { x := 95.0; in.CPUBusy15m = &x }, "warning host CPU 95% busy"},
		{"oom", func(in *Input) { in.Containers[0].OOMKilled = true }, "warning web container was killed"},
		{"dead", func(in *Input) { in.Containers[0].State = "dead" }, "critical web container is dead"},
	}
	for _, c := range cases {
		in := healthy()
		c.change(&in)
		got := Find(in)
		if c.want == "" {
			if len(got) != 0 {
				t.Errorf("%s: %+v", c.name, got)
			}
			continue
		}
		if len(got) != 1 || !strings.HasPrefix(got[0].Severity+" "+got[0].Subject+" "+got[0].Message, c.want) {
			t.Errorf("%s: %+v, want %q", c.name, got, c.want)
		}
	}
}

func TestCriticalFirst(t *testing.T) {
	in := healthy()
	in.LastError = "x"
	set(&in, "fs", "/", "avail_bytes", 1)
	got := Find(in)
	if len(got) != 2 || got[0].Severity != Critical {
		t.Fatalf("order %+v", got)
	}
}

// healthy is a host with 4 cores, 100-byte root disk half free, 100
// bytes of memory half available, a running container and fresh data.
func healthy() Input {
	values := []store.LatestValue{
		lv("fs", "/", "size_bytes", 100), lv("fs", "/", "avail_bytes", 50),
		lv("fs", "/", "inodes", 100), lv("fs", "/", "inodes_used", 10),
		lv("host", "", "mem_total_bytes", 100), lv("host", "", "mem_available_bytes", 50),
		lv("host", "", "swap_total_bytes", 100), lv("host", "", "swap_used_bytes", 0),
		lv("host", "", "load5", 1),
	}
	for _, core := range []string{"0", "1", "2", "3"} {
		values = append(values, lv("core", core, "busy_pct", 10))
	}
	busy := 20.0
	return Input{
		Now: now, PullInterval: 5 * time.Minute, LastSampleMs: now.Add(-time.Minute).UnixMilli(),
		Latest: values, CPUBusy15m: &busy,
		Containers: []store.ContainerState{{Name: "web", State: "running"}},
	}
}

func lv(kind, entity, metric string, v float64) store.LatestValue {
	return store.LatestValue{SeriesInfo: store.SeriesInfo{Kind: kind, Entity: entity, Metric: metric}, V: v}
}

// set changes one latest value of in.
func set(in *Input, kind, entity, metric string, v float64) {
	for i, l := range in.Latest {
		if l.Kind == kind && l.Entity == entity && l.Metric == metric {
			in.Latest[i].V = v
			return
		}
	}
	panic("no such value " + metric)
}
