package data

import (
	"mitosu/src/shell"
	"strings"

	"github.com/rs/zerolog/log"
)

type DockerContainer struct {
	ID       string
	Name     string
	CPU      string
	MemUsage string
	MemPerc  string
	NetIO    string
	BlockIO  string
	PIDs     string
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
			ID:       linesArr[i],
			Name:     linesArr[i+1],
			CPU:      linesArr[i+2],
			MemUsage: linesArr[i+3],
			MemPerc:  linesArr[i+4],
			NetIO:    linesArr[i+5],
			BlockIO:  linesArr[i+6],
			PIDs:     linesArr[i+7],
		}
		f.DockerContainers = append(f.DockerContainers, container)
	}
}
