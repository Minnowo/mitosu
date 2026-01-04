package data

import (
	"bufio"
	"mitosu/src/shell"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type CPURaw struct {
	User    uint64 // time spent in user mode
	Nice    uint64 // time spent in user mode with low priority (nice)
	System  uint64 // time spent in system mode
	Idle    uint64 // time spent in the idle task
	Iowait  uint64 // time spent waiting for I/O to complete (since Linux 2.5.41)
	Irq     uint64 // time spent servicing  interrupts  (since  2.6.0-test4)
	SoftIrq uint64 // time spent servicing softirqs (since 2.6.0-test4)
	Steal   uint64 // time spent in other OSes when running in a virtualized environment
	Guest   uint64 // time spent running a virtual CPU for guest operating systems under the control of the Linux kernel.
	Total   uint64 // total of all time fields
}

type CPUInfo struct {
	Total   uint64 // total of all time fields
	User    float32
	Nice    float32
	System  float32
	Idle    float32
	Iowait  float32
	Irq     float32
	SoftIrq float32
	Steal   float32
	Guest   float32
}

type ProcInfoSystemStat struct {
	Hostname string

	Uptime time.Duration

	CPU    CPUInfo
	CPURaw CPURaw

	Load1        string
	Load5        string
	Load10       string
	RunningProcs string
	TotalProcs   string
	MemTotal     uint64
	MemFree      uint64
	MemBuffers   uint64
	MemCached    uint64
	SwapTotal    uint64
	SwapFree     uint64
}

func (f *ProcInfoSystemStat) CmdCount(sh shell.ShellType) int {
	switch sh {
	default:
		log.Panic().Int("shellType", int(sh)).Msg("Unknown shell type given")
	case shell.PosixShellType:
		return 5
	}
	return 0
}

func (f *ProcInfoSystemStat) GetCmds(sh shell.ShellType) []shell.ShellCmd {

	cmds := make([]shell.ShellCmd, f.CmdCount(sh))

	switch sh {
	default:
		log.Panic().Int("shellType", int(sh)).Msg("Unknown shell type given")

	case shell.PosixShellType:

		cmds[0].Cmd = "hostname -f"
		cmds[1].Cmd = "cat /proc/uptime"
		cmds[2].Cmd = "cat /proc/loadavg"
		cmds[3].Cmd = "cat /proc/meminfo"
		cmds[4].Cmd = "cat /proc/stat"

	}

	return cmds
}

func (f *ProcInfoSystemStat) ParseCmdOutput(sh shell.ShellType, outs []string) {

	if len(outs) < 1 {
		log.Debug().Msg("Could not parse system proc stats")
		return
	}

	var err error

	err = f.getHostname(outs[0])
	log.Debug().Err(err).Msg("Parsing hostname")

	if len(outs) < 2 {
		log.Debug().Msg("Could only parse 1 of 5 proc stats")
		return
	}

	err = f.getUptime(outs[1])
	log.Debug().Err(err).Msg("Parsing uptime")

	if len(outs) < 3 {
		log.Debug().Msg("Could only parse 2 of 5 proc stats")
		return
	}

	err = f.getLoad(outs[2])
	log.Debug().Err(err).Msg("Parsing load")

	if len(outs) < 4 {
		log.Debug().Msg("Could only parse 3 of 5 proc stats")
		return
	}

	err = f.getMemInfo(outs[3])
	log.Debug().Err(err).Msg("Parsing memory info")

	if len(outs) < 4 {
		log.Debug().Msg("Could only parse 4 of 5 proc stats")
		return
	}

	err = f.getCPU(outs[4])
	log.Debug().Err(err).Msg("Parsing CPU")
}

func (f *ProcInfoSystemStat) getHostname(hostname string) error {

	f.Hostname = strings.TrimSpace(hostname)

	return nil
}

func (f *ProcInfoSystemStat) getUptime(uptime string) error {

	parts := strings.Fields(uptime)

	if len(parts) == 2 {

		var upsecs float64
		upsecs, err := strconv.ParseFloat(parts[0], 64)

		if err != nil {
			return err
		}
		f.Uptime = time.Duration(upsecs * 1e9)
	}

	return nil
}

func (f *ProcInfoSystemStat) getLoad(line string) error {

	parts := strings.Fields(line)

	if len(parts) == 5 {
		f.Load1 = parts[0]
		f.Load5 = parts[1]
		f.Load10 = parts[2]
		if i := strings.Index(parts[3], "/"); i != -1 {
			f.RunningProcs = parts[3][0:i]
			if i+1 < len(parts[3]) {
				f.TotalProcs = parts[3][i+1:]
			}
		}
	}

	return nil
}

func (f *ProcInfoSystemStat) getMemInfo(lines string) error {

	scanner := bufio.NewScanner(strings.NewReader(lines))

	for scanner.Scan() {

		line := scanner.Text()

		parts := strings.Fields(line)

		if len(parts) != 3 {
			continue
		}

		val, err := strconv.ParseUint(parts[1], 10, 64)

		if err != nil {
			continue
		}

		val *= 1024

		switch parts[0] {
		case "MemTotal:":
			f.MemTotal = val
		case "MemFree:":
			f.MemFree = val
		case "Buffers:":
			f.MemBuffers = val
		case "Cached:":
			f.MemCached = val
		case "SwapTotal:":
			f.SwapTotal = val
		case "SwapFree:":
			f.SwapFree = val
		}
	}

	return nil
}

func (f *ProcInfoSystemStat) getCPU(lines string) error {

	var (
		nowCPU CPURaw
		total  float32
	)

	scanner := bufio.NewScanner(strings.NewReader(lines))

	for scanner.Scan() {

		line := scanner.Text()

		fields := strings.Fields(line)
		numFields := len(fields)

		if numFields <= 0 || fields[0] != "cpu" { // changing here if want to get every cpu-core's stats
			continue
		}

		for i := 1; i < numFields; i++ {

			val, err := strconv.ParseUint(fields[i], 10, 64)

			if err != nil {
				continue
			}

			nowCPU.Total += val
			switch i {
			case 1:
				nowCPU.User = val
			case 2:
				nowCPU.Nice = val
			case 3:
				nowCPU.System = val
			case 4:
				nowCPU.Idle = val
			case 5:
				nowCPU.Iowait = val
			case 6:
				nowCPU.Irq = val
			case 7:
				nowCPU.SoftIrq = val
			case 8:
				nowCPU.Steal = val
			case 9:
				nowCPU.Guest = val
			}
		}

		break
	}

	preCPU := f.CPURaw

	if preCPU.Total != 0 { // having no pre raw cpu data
		total = float32(nowCPU.Total - preCPU.Total)
		f.CPU.User = float32(nowCPU.User-preCPU.User) / total * 100
		f.CPU.Nice = float32(nowCPU.Nice-preCPU.Nice) / total * 100
		f.CPU.System = float32(nowCPU.System-preCPU.System) / total * 100
		f.CPU.Idle = float32(nowCPU.Idle-preCPU.Idle) / total * 100
		f.CPU.Iowait = float32(nowCPU.Iowait-preCPU.Iowait) / total * 100
		f.CPU.Irq = float32(nowCPU.Irq-preCPU.Irq) / total * 100
		f.CPU.SoftIrq = float32(nowCPU.SoftIrq-preCPU.SoftIrq) / total * 100
		f.CPU.Guest = float32(nowCPU.Guest-preCPU.Guest) / total * 100
		f.CPU.Total = nowCPU.Total
	}

	f.CPURaw = nowCPU

	return nil
}
