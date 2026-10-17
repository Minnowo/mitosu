// A container's configuration (docker inspect): only the listed fields
// are decoded. Environment variables, command lines and labels other than
// Compose's are never read, so secrets in them cannot reach the UI.

package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ContainerInfo is what the UI shows of a container's configuration.
type ContainerInfo struct {
	Name          string      `json:"name"`
	Image         string      `json:"image"`
	ImageID       string      `json:"image_id"`
	Created       string      `json:"created"`
	State         string      `json:"state"`
	Health        string      `json:"health"`
	StartedAt     string      `json:"started_at"`
	FinishedAt    string      `json:"finished_at"`
	ExitCode      int         `json:"exit_code"`
	OOMKilled     bool        `json:"oom_killed"`
	RestartCount  int         `json:"restart_count"`
	RestartPolicy string      `json:"restart_policy"`
	Project       string      `json:"project"`
	Service       string      `json:"service"`
	Ports         []PortMap   `json:"ports"`
	Mounts        []MountInfo `json:"mounts"`
	Networks      []NetInfo   `json:"networks"`
}

// PortMap is a published port.
type PortMap struct {
	Container string `json:"container"`
	HostIP    string `json:"host_ip"`
	HostPort  string `json:"host_port"`
}

// MountInfo is one mount.
type MountInfo struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadWrite   bool   `json:"rw"`
}

// NetInfo is one network the container is on.
type NetInfo struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip"`
}

// rawInspect lists the only fields decoded from docker inspect.
type rawInspect struct {
	Name    string
	Image   string
	Created string
	State   struct {
		Status     string
		StartedAt  string
		FinishedAt string
		ExitCode   int
		OOMKilled  bool
		Health     *struct{ Status string }
	}
	RestartCount int
	Config       struct {
		Image  string
		Labels map[string]string
	}
	HostConfig struct {
		RestartPolicy struct {
			Name              string
			MaximumRetryCount int
		}
	}
	Mounts []struct {
		Type, Source, Destination string
		RW                        bool
	}
	NetworkSettings struct {
		Ports    map[string][]struct{ HostIp, HostPort string }
		Networks map[string]struct{ IPAddress string }
	}
}

// Inspect reads container's configuration on host.
func Inspect(ctx context.Context, r Runner, o Options, host, container string) (ContainerInfo, error) {
	if err := checkContainer(container); err != nil {
		return ContainerInfo{}, err
	}
	out, err := run(ctx, r, host, "docker", o.sudo()+"docker inspect --type container -- "+quote(container))
	if err != nil {
		return ContainerInfo{}, err
	}
	if out.truncated {
		return ContainerInfo{}, errors.New("docker inspect printed more than expected")
	}
	return parseInspect(out.stdout)
}

// parseInspect turns docker inspect's JSON (an array of one) into a
// ContainerInfo.
func parseInspect(data []byte) (ContainerInfo, error) {
	var raw []rawInspect
	if err := json.Unmarshal(data, &raw); err != nil {
		if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] != '[' {
			return ContainerInfo{}, errors.New("the login printed text before docker's output; shell startup files (such as ~/.bashrc) must print to stderr or nothing")
		}
		return ContainerInfo{}, fmt.Errorf("docker inspect: %w", err)
	}
	if len(raw) != 1 {
		return ContainerInfo{}, fmt.Errorf("docker inspect returned %d containers", len(raw))
	}
	d := raw[0]
	info := ContainerInfo{
		Name: trimSlash(d.Name), Image: d.Config.Image, ImageID: d.Image, Created: d.Created,
		State: d.State.Status, StartedAt: d.State.StartedAt, FinishedAt: d.State.FinishedAt,
		ExitCode: d.State.ExitCode, OOMKilled: d.State.OOMKilled, RestartCount: d.RestartCount,
		RestartPolicy: d.HostConfig.RestartPolicy.Name,
		Project:       d.Config.Labels["com.docker.compose.project"],
		Service:       d.Config.Labels["com.docker.compose.service"],
		Ports:         []PortMap{}, Mounts: []MountInfo{}, Networks: []NetInfo{},
	}
	if d.State.Health != nil {
		info.Health = d.State.Health.Status
	}
	if d.HostConfig.RestartPolicy.MaximumRetryCount > 0 {
		info.RestartPolicy += fmt.Sprintf(":%d", d.HostConfig.RestartPolicy.MaximumRetryCount)
	}
	for port, binds := range d.NetworkSettings.Ports {
		if len(binds) == 0 {
			info.Ports = append(info.Ports, PortMap{Container: port})
		}
		for _, b := range binds {
			info.Ports = append(info.Ports, PortMap{Container: port, HostIP: b.HostIp, HostPort: b.HostPort})
		}
	}
	for _, m := range d.Mounts {
		info.Mounts = append(info.Mounts, MountInfo{Type: m.Type, Source: m.Source, Destination: m.Destination, ReadWrite: m.RW})
	}
	for name, n := range d.NetworkSettings.Networks {
		info.Networks = append(info.Networks, NetInfo{Name: name, IPAddress: n.IPAddress})
	}
	sortInfo(&info)
	return info, nil
}

func trimSlash(s string) string {
	if len(s) > 0 && s[0] == '/' {
		return s[1:]
	}
	return s
}

// sortInfo orders the lists, which come from JSON maps in random order.
func sortInfo(info *ContainerInfo) {
	sort.Slice(info.Ports, func(i, j int) bool {
		a, b := info.Ports[i], info.Ports[j]
		return a.Container+a.HostIP+a.HostPort < b.Container+b.HostIP+b.HostPort
	})
	sort.Slice(info.Networks, func(i, j int) bool { return info.Networks[i].Name < info.Networks[j].Name })
}
