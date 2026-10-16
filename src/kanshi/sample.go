// The sample line of the kanshi protocol, format 1.x
// (../kanshi/docs/protocol.md, "sample"). Optional sections are pointers:
// nil means the section was absent ("unknown at this time"), which a
// reader must never treat as zero or empty.

package kanshi

// Sample is one measurement of the server.
type Sample struct {
	Seq        uint64       `json:"seq"`
	TimeMs     uint64       `json:"time_ms"`
	BootID     *string      `json:"boot_id"`
	UptimeMs   *uint64      `json:"uptime_ms"`
	CPU        *CPU         `json:"cpu"`
	Mem        *Mem         `json:"mem"`
	Load       *Load        `json:"load"`
	Vmstat     *Vmstat      `json:"vmstat"`
	FS         *[]FS        `json:"fs"`
	Disks      *[]Disk      `json:"disks"`
	Net        *[]NetIf     `json:"net"`
	Containers *[]Container `json:"containers"`
	Errors     *[]Problem   `json:"errors"`
}

// CPUTimes are cumulative CPU time counters in USER_HZ ticks.
type CPUTimes struct {
	User    uint64 `json:"user"`
	Nice    uint64 `json:"nice"`
	System  uint64 `json:"system"`
	Idle    uint64 `json:"idle"`
	Iowait  uint64 `json:"iowait"`
	Irq     uint64 `json:"irq"`
	Softirq uint64 `json:"softirq"`
	Steal   uint64 `json:"steal"`
}

// Core is one CPU's counters; ID is the kernel's CPU number.
type Core struct {
	ID uint64 `json:"id"`
	CPUTimes
}

// CPU holds the whole machine's counters and each online core's.
type CPU struct {
	Total CPUTimes `json:"total"`
	Cores []Core   `json:"cores"`
}

// Mem holds /proc/meminfo gauges in KiB.
type Mem struct {
	TotalKiB     uint64 `json:"total_kib"`
	FreeKiB      uint64 `json:"free_kib"`
	AvailableKiB uint64 `json:"available_kib"`
	BuffersKiB   uint64 `json:"buffers_kib"`
	CachedKiB    uint64 `json:"cached_kib"`
	SwapTotalKiB uint64 `json:"swap_total_kib"`
	SwapFreeKiB  uint64 `json:"swap_free_kib"`
}

// Load holds the load averages times 100 and task counts.
type Load struct {
	Avg1x100  uint64 `json:"avg1_x100"`
	Avg5x100  uint64 `json:"avg5_x100"`
	Avg15x100 uint64 `json:"avg15_x100"`
	Runnable  uint64 `json:"runnable"`
	Tasks     uint64 `json:"tasks"`
}

// Vmstat holds swap page counters.
type Vmstat struct {
	Pswpin  uint64 `json:"pswpin"`
	Pswpout uint64 `json:"pswpout"`
}

// FS is one mounted block filesystem; sizes are gauges in bytes.
type FS struct {
	Mount      string `json:"mount"`
	Source     string `json:"source"`
	Type       string `json:"type"`
	SizeBytes  uint64 `json:"size_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	AvailBytes uint64 `json:"avail_bytes"`
	Inodes     uint64 `json:"inodes"`
	InodesFree uint64 `json:"inodes_free"`
}

// Disk is one whole block device's counters (sectors are 512 bytes;
// the _ms fields and InFlight are 32-bit in the kernel and wrap).
type Disk struct {
	Name         string `json:"name"`
	Reads        uint64 `json:"reads"`
	ReadSectors  uint64 `json:"read_sectors"`
	ReadMs       uint64 `json:"read_ms"`
	Writes       uint64 `json:"writes"`
	WriteSectors uint64 `json:"write_sectors"`
	WriteMs      uint64 `json:"write_ms"`
	InFlight     uint64 `json:"in_flight"`
	IoMs         uint64 `json:"io_ms"`
}

// NetCounters are network interface counters.
type NetCounters struct {
	RxBytes   uint64 `json:"rx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	RxErrs    uint64 `json:"rx_errs"`
	RxDrop    uint64 `json:"rx_drop"`
	TxBytes   uint64 `json:"tx_bytes"`
	TxPackets uint64 `json:"tx_packets"`
	TxErrs    uint64 `json:"tx_errs"`
	TxDrop    uint64 `json:"tx_drop"`
}

// NetIf is one host network interface.
type NetIf struct {
	Name string `json:"name"`
	NetCounters
}

// Container is one Docker container. Project, Service and Health are
// empty when absent (omitempty marks them optional for the reader's
// required-field check); Stats is nil when absent.
type Container struct {
	Name         string          `json:"name"`
	ID           string          `json:"id"`
	Project      string          `json:"project,omitempty"`
	Service      string          `json:"service,omitempty"`
	Image        string          `json:"image"`
	State        string          `json:"state"`
	Health       string          `json:"health,omitempty"`
	Restarts     uint64          `json:"restarts"`
	StartedAtMs  uint64          `json:"started_at_ms"`
	FinishedAtMs uint64          `json:"finished_at_ms"`
	ExitCode     int64           `json:"exit_code"`
	OOMKilled    bool            `json:"oom_killed"`
	Stats        *ContainerStats `json:"stats"`
}

// ContainerStats are a running container's counters and gauges.
type ContainerStats struct {
	CPUNs                uint64       `json:"cpu_ns"`
	MemBytes             uint64       `json:"mem_bytes"`
	MemInactiveFileBytes uint64       `json:"mem_inactive_file_bytes"`
	MemLimitBytes        uint64       `json:"mem_limit_bytes"`
	Pids                 uint64       `json:"pids"`
	BlkReadBytes         uint64       `json:"blk_read_bytes"`
	BlkWriteBytes        uint64       `json:"blk_write_bytes"`
	Net                  *NetCounters `json:"net"`
}

// Problem is one entry of a sample's errors list: which section failed
// and a message for humans (do not parse it).
type Problem struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}
