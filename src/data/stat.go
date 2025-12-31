
// Taken and modified from
// https://github.com/rapidloop/rtop/blob/master/stats.go

package data

import (
	"bufio"
	"mitosu/src/ssh"
	"strconv"
	"strings"
	"time"
)

type FSInfo struct {
	MountPoint string
	Used       uint64
	Free       uint64
}

type NetIntfInfo struct {
	IPv4 string
	IPv6 string
	Rx   uint64
	Tx   uint64
}

type cpuRaw struct {
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

type Stats struct {
	Uptime       time.Duration
	Hostname     string
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
	FSInfos      []FSInfo
	NetIntf      map[string]NetIntfInfo
	CPU          CPUInfo // or []CPUInfo to get all the cpu-core's stats?
	preCPU cpuRaw
}

func GetAllStats(client ssh.SSHClient, stats *Stats) {
	getUptime(client, stats)
	getHostname(client, stats)
	getLoad(client, stats)
	getMemInfo(client, stats)
	getFSInfo(client, stats)
	getInterfaces(client, stats)
	getInterfaceInfo(client, stats)
	getCPU(client, stats)
}

func getUptime(client ssh.SSHClient, stats *Stats) (error) {

	uptime, err := client.RunCommand("/bin/cat /proc/uptime")

	if err != nil {
		return err 
	}

	parts := strings.Fields(uptime)

	if len(parts) == 2 {

		var upsecs float64
		upsecs, err = strconv.ParseFloat(parts[0], 64)

		if err != nil {
			return err
		}
		stats.Uptime = time.Duration(upsecs * 1e9)
	}

	return nil
}

func getHostname(client ssh.SSHClient, stats *Stats) (error) {

	hostname, err := client.RunCommand( "/bin/hostname -f")

	if err != nil {
		return err 
	}

	stats.Hostname = strings.TrimSpace(hostname)

	return nil
}

func getLoad(client ssh.SSHClient, stats *Stats) (error) {

	line, err := client.RunCommand("/bin/cat /proc/loadavg")

	if err != nil {
		return err
	}

	parts := strings.Fields(line)

	if len(parts) == 5 {
		stats.Load1 = parts[0]
		stats.Load5 = parts[1]
		stats.Load10 = parts[2]
		if i := strings.Index(parts[3], "/"); i != -1 {
			stats.RunningProcs = parts[3][0:i]
			if i+1 < len(parts[3]) {
				stats.TotalProcs = parts[3][i+1:]
			}
		}
	}

	return nil
}

func getMemInfo(client ssh.SSHClient, stats *Stats) (error) {

	lines, err := client.RunCommand("/bin/cat /proc/meminfo")

	if err != nil {
		return err
	}

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
			stats.MemTotal = val
		case "MemFree:":
			stats.MemFree = val
		case "Buffers:":
			stats.MemBuffers = val
		case "Cached:":
			stats.MemCached = val
		case "SwapTotal:":
			stats.SwapTotal = val
		case "SwapFree:":
			stats.SwapFree = val
		}
	}

	return nil
}

func getFSInfo(client ssh.SSHClient, stats *Stats) (error) {

	lines, err :=client.RunCommand( "/bin/df -B1")

	if err != nil {
		return err 
	}

	stats.FSInfos = stats.FSInfos[:0]

	scanner := bufio.NewScanner(strings.NewReader(lines))

	flag := 0
	for scanner.Scan() {

		line := scanner.Text()
		parts := strings.Fields(line)
		n := len(parts)
		dev := n > 0 && strings.Index(parts[0], "/dev/") == 0

		if n == 1 && dev {
			flag = 1
		} else if (n == 5 && flag == 1) || (n == 6 && dev) {
			i := flag
			flag = 0

			used, err := strconv.ParseUint(parts[2-i], 10, 64)

			if err != nil {
				continue
			}

			free, err := strconv.ParseUint(parts[3-i], 10, 64)

			if err != nil {
				continue
			}

			stats.FSInfos = append(stats.FSInfos, FSInfo{
				parts[5-i], used, free,
			})
		}
	}

	return nil
}

func getInterfaces(client ssh.SSHClient, stats *Stats) (error) {

	var lines string

	lines, err := client.RunCommand("/bin/ip -o addr")

	if err != nil {

		// try /sbin/ip
		lines, err = client.RunCommand("/sbin/ip -o addr")

		if err != nil {
			return err
		}
	}

	if stats.NetIntf == nil {
		stats.NetIntf = make(map[string]NetIntfInfo)
	}

	for k := range stats.NetIntf {
		delete(stats.NetIntf, k)
	}

	scanner := bufio.NewScanner(strings.NewReader(lines))

	for scanner.Scan() {

		line := scanner.Text()
		parts := strings.Fields(line)

		if len(parts) >= 4 && (parts[2] == "inet" || parts[2] == "inet6") {

			ipv4 := parts[2] == "inet"
			intfname := parts[1]

			if info, ok := stats.NetIntf[intfname]; ok {
				if ipv4 {
					info.IPv4 = parts[3]
				} else {
					info.IPv6 = parts[3]
				}
				stats.NetIntf[intfname] = info
			} else {
				info := NetIntfInfo{}
				if ipv4 {
					info.IPv4 = parts[3]
				} else {
					info.IPv6 = parts[3]
				}
				stats.NetIntf[intfname] = info
			}
		}
	}

	return nil
}

func getInterfaceInfo(client ssh.SSHClient, stats *Stats) (error) {

	lines, err :=client.RunCommand( "/bin/cat /proc/net/dev")

	if err != nil {
		return err 
	}

	if stats.NetIntf == nil {
		return err
	} // should have been here already

	scanner := bufio.NewScanner(strings.NewReader(lines))

	for scanner.Scan() {

		line := scanner.Text()
		parts := strings.Fields(line)

		if len(parts) != 17 {
			continue
		}
		intf := strings.TrimSpace(parts[0])
		intf = strings.TrimSuffix(intf, ":")

		if info, ok := stats.NetIntf[intf]; ok {

			rx, err := strconv.ParseUint(parts[1], 10, 64)

			if err != nil {
				continue
			}

			tx, err := strconv.ParseUint(parts[9], 10, 64)

			if err != nil {
				continue
			}
			info.Rx = rx
			info.Tx = tx
			stats.NetIntf[intf] = info
		}
	}

	return nil
}

func parseCPUFields(fields []string, stat *cpuRaw) {

	numFields := len(fields)

	for i := 1; i < numFields; i++ {

		val, err := strconv.ParseUint(fields[i], 10, 64)

		if err != nil {
			continue
		}

		stat.Total += val
		switch i {
		case 1:
			stat.User = val
		case 2:
			stat.Nice = val
		case 3:
			stat.System = val
		case 4:
			stat.Idle = val
		case 5:
			stat.Iowait = val
		case 6:
			stat.Irq = val
		case 7:
			stat.SoftIrq = val
		case 8:
			stat.Steal = val
		case 9:
			stat.Guest = val
		}
	}
}

func getCPU(client ssh.SSHClient, stats *Stats) (error) {

	lines, err := client.RunCommand("/bin/cat /proc/stat")

	if err != nil {
		return err
	}

	var (
		nowCPU cpuRaw
		total  float32
	)

	scanner := bufio.NewScanner(strings.NewReader(lines))

	for scanner.Scan() {

		line := scanner.Text()

		fields := strings.Fields(line)

		if len(fields) > 0 && fields[0] == "cpu" { // changing here if want to get every cpu-core's stats
			parseCPUFields(fields, &nowCPU)
			break
		}
	}

	preCPU := stats.preCPU

	if preCPU.Total != 0 { // having no pre raw cpu data
		total = float32(nowCPU.Total - preCPU.Total)
		stats.CPU.User = float32(nowCPU.User-preCPU.User) / total * 100
		stats.CPU.Nice = float32(nowCPU.Nice-preCPU.Nice) / total * 100
		stats.CPU.System = float32(nowCPU.System-preCPU.System) / total * 100
		stats.CPU.Idle = float32(nowCPU.Idle-preCPU.Idle) / total * 100
		stats.CPU.Iowait = float32(nowCPU.Iowait-preCPU.Iowait) / total * 100
		stats.CPU.Irq = float32(nowCPU.Irq-preCPU.Irq) / total * 100
		stats.CPU.SoftIrq = float32(nowCPU.SoftIrq-preCPU.SoftIrq) / total * 100
		stats.CPU.Guest = float32(nowCPU.Guest-preCPU.Guest) / total * 100
	}

	stats.preCPU = nowCPU

	return nil
}
