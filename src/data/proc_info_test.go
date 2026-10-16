// Tests for the /proc parsers behind `mitosu stat`, fed with output as a
// server would return it, including cut-off and hostile output.

package data

import (
	"math"
	"testing"

	"mitosu/src/shell"
)

const (
	procHostname = "web1\n"
	procUptime   = "350735.47 234388.90\n"
	procLoadavg  = "0.12 0.20 0.18 2/487 12345\n"
	procMeminfo  = "MemTotal:        7944056 kB\nMemFree:         5592548 kB\n"
	procStatA    = "cpu  100 0 50 800 10 0 5 35 0 0\n"
	procStatB    = "cpu  200 0 100 1500 20 0 10 70 0 0\n"
)

func TestProcInfoWithFourOutputsDoesNotPanic(t *testing.T) {
	var stat ProcInfoSystemStat
	stat.ParseCmdOutput(shell.PosixShellType, []string{procHostname, procUptime, procLoadavg, procMeminfo})
	if stat.MemTotal != 7944056*1024 {
		t.Fatalf("MemTotal = %d", stat.MemTotal)
	}
}

func TestProcInfoCPUPercentages(t *testing.T) {
	var stat ProcInfoSystemStat
	parseProc(&stat, procStatA)
	parseProc(&stat, procStatB)
	// Deltas: user 100, system 50, idle 700, iowait 10, softirq 5, steal 35; total 900.
	checkPercent(t, "User", stat.CPU.User, 100.0/9)
	checkPercent(t, "System", stat.CPU.System, 50.0/9)
	checkPercent(t, "Idle", stat.CPU.Idle, 700.0/9)
	checkPercent(t, "Steal", stat.CPU.Steal, 35.0/9)
}

func TestProcInfoCPUCounterResetGivesNoRate(t *testing.T) {
	var stat ProcInfoSystemStat
	parseProc(&stat, procStatB)
	parseProc(&stat, procStatA)
	if stat.CPU != (CPUInfo{}) {
		t.Fatalf("after a counter reset CPU = %+v, want no rate", stat.CPU)
	}
}

func TestProcInfoCPUUnchangedCountersGiveNoRate(t *testing.T) {
	var stat ProcInfoSystemStat
	parseProc(&stat, procStatA)
	parseProc(&stat, procStatA)
	if stat.CPU != (CPUInfo{}) {
		t.Fatalf("with no elapsed CPU time CPU = %+v, want no rate", stat.CPU)
	}
}

func parseProc(stat *ProcInfoSystemStat, procStat string) {
	stat.ParseCmdOutput(shell.PosixShellType, []string{procHostname, procUptime, procLoadavg, procMeminfo, procStat})
}

func checkPercent(t *testing.T, name string, got float32, want float64) {
	t.Helper()
	if math.Abs(float64(got)-want) > 0.01 {
		t.Errorf("%s = %f, want %f", name, got, want)
	}
}
