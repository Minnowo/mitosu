// Facts and pending work on the server, in one round trip: OS (os-
// release(5)), kernel, CPU model and count, uptime, Docker version,
// upgradable packages (apt list --upgradable, from the existing package
// lists: apt update is never run), whether a reboot is required
// (/var/run/reboot-required, written by Debian and Ubuntu packages) and
// failed systemd units. A part that fails is left empty; the rest is
// still shown.

package live

import (
	"bufio"
	"context"
	"strconv"
	"strings"
)

// marker starts each part of the script's output; no part can print it.
const marker = "@@mitosu-part:"

// maxListed caps upgradable packages and failed units.
const maxListed = 2000

// System is what the system view shows.
type System struct {
	OS            string    `json:"os"`
	Kernel        string    `json:"kernel"`
	CPUModel      string    `json:"cpu_model"`
	CPUs          int       `json:"cpus"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	DockerVersion string    `json:"docker_version"`
	Updates       []Upgrade `json:"updates"`
	// RebootRequired is true when /var/run/reboot-required exists;
	// RebootPackages are the packages that asked for it.
	RebootRequired bool     `json:"reboot_required"`
	RebootPackages []string `json:"reboot_packages"`
	FailedUnits    []string `json:"failed_units"`
}

// Upgrade is one upgradable package.
type Upgrade struct {
	Package string `json:"package"`
	Version string `json:"version"`
	From    string `json:"from"`
}

// systemScript builds the script; docker runs through sudo when asked.
func systemScript(o Options) string {
	parts := []struct{ name, cmd string }{
		{"os", "cat /etc/os-release"},
		{"kernel", "uname -srm"},
		{"cpus", "nproc"},
		{"cpu", "grep -m1 '^model name' /proc/cpuinfo"},
		{"uptime", "cat /proc/uptime"},
		{"docker", o.sudo() + "docker version --format '{{.Server.Version}}'"},
		{"updates", "apt list --upgradable"},
		{"reboot", "if [ -f /var/run/reboot-required ]; then echo yes; cat /var/run/reboot-required.pkgs; fi"},
		{"failed", "systemctl --failed --no-legend --plain"},
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString("echo '" + marker + p.name + "'; " + p.cmd + " 2>/dev/null")
	}
	// The exit status is not that of whichever part ran last.
	b.WriteString("; true")
	return b.String()
}

// SystemView reads the system facts of host.
func SystemView(ctx context.Context, r Runner, o Options, host string) (System, error) {
	out, err := run(ctx, r, host, "sh", systemScript(o), false)
	if err != nil {
		return System{}, err
	}
	return parseSystem(string(out.stdout)), nil
}

// parseSystem reads the script's output.
func parseSystem(text string) System {
	s := System{Updates: []Upgrade{}, RebootPackages: []string{}, FailedUnits: []string{}}
	part := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), maxOutput)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, marker) {
			part = strings.TrimPrefix(line, marker)
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch part {
		case "os":
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				s.OS = strings.Trim(v, `"'`)
			}
		case "kernel":
			s.Kernel = line
		case "cpus":
			s.CPUs, _ = strconv.Atoi(line)
		case "cpu":
			if _, v, ok := strings.Cut(line, ":"); ok {
				s.CPUModel = strings.TrimSpace(v)
			}
		case "uptime":
			if f := strings.Fields(line); len(f) > 0 {
				if secs, err := strconv.ParseFloat(f[0], 64); err == nil && secs >= 0 {
					s.UptimeSeconds = int64(secs)
				}
			}
		case "docker":
			s.DockerVersion = line
		case "updates":
			if u, ok := parseUpgrade(line); ok && len(s.Updates) < maxListed {
				s.Updates = append(s.Updates, u)
			}
		case "reboot":
			if line == "yes" {
				s.RebootRequired = true
			} else if len(s.RebootPackages) < maxListed {
				s.RebootPackages = append(s.RebootPackages, line)
			}
		case "failed":
			if f := strings.Fields(line); len(f) > 0 && len(s.FailedUnits) < maxListed {
				s.FailedUnits = append(s.FailedUnits, f[0])
			}
		}
	}
	return s
}

// parseUpgrade reads "name/suite version arch [upgradable from: old]".
func parseUpgrade(line string) (Upgrade, bool) {
	f := strings.Fields(line)
	if len(f) < 2 || !strings.Contains(f[0], "/") {
		return Upgrade{}, false
	}
	name, _, _ := strings.Cut(f[0], "/")
	u := Upgrade{Package: name, Version: f[1]}
	if _, from, ok := strings.Cut(line, "upgradable from: "); ok {
		u.From = strings.TrimSuffix(from, "]")
	}
	return u, true
}
