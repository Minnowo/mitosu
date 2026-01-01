package data

import (
	"fmt"
	"mitosu/src/shell"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type DockerContainer struct {
	ID       string
	Name     string
	CPU      string
	MemUsed  uint64
	MemTotal uint64

	MemPerc  string
	NetIn    uint64
	NetOut   uint64
	BlockIn  uint64
	BlockOut uint64
	PIDs     uint64
}

type DockerSystemStat struct {
	DockerContainers []DockerContainer
}

func (f *DockerSystemStat) CmdCount(sh shell.ShellType) int {
	switch sh {

	default:
		log.Panic().Int("shellType", int(sh)).Msg("Unknown shell type given")

	case shell.PosixShellType:
		return 1
	}
	return 0
}
func (f *DockerSystemStat) GetCmds(sh shell.ShellType) []shell.ShellCmd {

	var cmd shell.ShellCmd

	switch sh {
	default:
	case shell.PosixShellType:

		cmd.Cmd = `docker stats --no-stream --format "` +
			`{{.Container}}\n` +
			`{{.Name}}\n` +
			`{{.CPUPerc}}\n` +
			`{{.MemUsage}}\n` +
			`{{.MemPerc}}\n` +
			`{{.NetIO}}\n` +
			`{{.BlockIO}}\n` +
			`{{.PIDs}}` +
			`"`
		cmd.Stdin = nil
	}

	return []shell.ShellCmd{cmd}
}

func (f *DockerSystemStat) ParseCmdOutput(sh shell.ShellType, outs []string) {

	if len(outs) < 1 {
		log.Debug().Msg("Cannot parse docker containers because no output")
		return
	}

	if f.DockerContainers == nil {
		f.DockerContainers = make([]DockerContainer, 0)
	} else {
		f.DockerContainers = f.DockerContainers[:0]
	}

	linesArr := strings.Split(strings.TrimSpace(outs[0]), "\n")

	const fields = 8

	for i := 0; i+fields <= len(linesArr); i += fields {

		container := DockerContainer{
			ID:      linesArr[i],
			Name:    linesArr[i+1],
			CPU:     linesArr[i+2],
			MemPerc: linesArr[i+4],
		}

		// memory used / total
		if mem := strings.Split(linesArr[i+3], "/"); len(mem) == 2 {

			if n, err := parseMemory(mem[0]); err == nil {
				container.MemUsed = n
			} else {
				log.Debug().Err(err).Msg("failed to parse docker container memory used")
			}

			if n, err := parseMemory(mem[1]); err == nil {
				container.MemTotal = n
			} else {
				log.Debug().Err(err).Msg("failed to parse docker container memory total")
			}
		}

		// network io in/out
		if mem := strings.Split(linesArr[i+5], "/"); len(mem) == 2 {

			if n, err := parseMemory(mem[0]); err == nil {
				container.NetIn = n
			} else {
				log.Debug().Err(err).Msg("failed to parse docker container network in")
			}

			if n, err := parseMemory(mem[1]); err == nil {
				container.NetOut = n
			} else {
				log.Debug().Err(err).Msg("failed to parse docker container network out")
			}

		}

		// block io in/out
		if mem := strings.Split(linesArr[i+6], "/"); len(mem) == 2 {

			if n, err := parseMemory(mem[0]); err == nil {
				container.BlockIn = n
			} else {
				log.Debug().Err(err).Msg("failed to parse docker container block io in")
			}

			if n, err := parseMemory(mem[1]); err == nil {
				container.BlockOut = n
			} else {
				log.Debug().Err(err).Msg("failed to parse docker container block io out")
			}
		}

		if n, err := strconv.Atoi(strings.TrimSpace(linesArr[i+7])); err == nil {
			container.PIDs = uint64(n)
		} else {
			log.Debug().Err(err).Msg("failed to parse docker container PIDs")
		}

		f.DockerContainers = append(f.DockerContainers, container)
	}
}

func parseMemory(s string) (uint64, error) {

	var value float64
	var unit string

	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%f%s", &value, &unit); err != nil {
		return 0, err
	}

	log.Debug().Str("raw", s).Str("unit", unit).Float64("val", value).Msg("docker parsing memory value")

	switch strings.ToLower(unit)[0] {
	case 'b':
		return uint64(value), nil
	case 'k':
		return uint64(value * 1024), nil
	case 'm':
		return uint64(value * 1024 * 1024), nil
	case 'g':
		return uint64(value * 1024 * 1024 * 1024), nil
	}
	return 0, fmt.Errorf("unknown unit: %s", unit)
}
