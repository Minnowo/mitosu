package data

import (
	"bufio"
	"mitosu/src/shell"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type FSInfo struct {
	MountPoint string
	Used       uint64
	Free       uint64
}

type FSSystemStat struct {
	FSInfos []FSInfo
}

func (f *FSSystemStat) CmdCount(sh shell.ShellType) int {
	switch sh {

	default:
		log.Panic().Int("shellType", int(sh)).Msg("Unknown shell type given")

	case shell.PosixShellType:
		return 1
	}
	return 0
}
func (f *FSSystemStat) GetCmds(sh shell.ShellType) []shell.ShellCmd {

	var cmd shell.ShellCmd

	switch sh {
	default:
	case shell.PosixShellType:
		cmd.Cmd = "df -B1"
		cmd.Stdin = nil
	}

	return []shell.ShellCmd{cmd}
}

func (f *FSSystemStat) ParseCmdOutput(sh shell.ShellType, outs []string) {

	if len(outs) < 1 {
		log.Debug().Msg("Parsing outputs failed because it was nil or lacked enough columns")
		return
	}

	if f.FSInfos == nil {
		f.FSInfos = make([]FSInfo, 0)
	} else {
		f.FSInfos = f.FSInfos[:0]
	}

	scanner := bufio.NewScanner(strings.NewReader(outs[0]))

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

			f.FSInfos = append(f.FSInfos, FSInfo{
				parts[5-i], used, free,
			})
		}
	}
}
