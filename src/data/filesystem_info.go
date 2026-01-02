package data

import (
	"bufio"
	"mitosu/src/shell"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type FSType int

var (
	FS_NULL  FSType = 0
	FS_Local FSType = 1
	FS_Net   FSType = 2
	FS_Other FSType = 3
)

func (f FSType) String() string {
	switch f {
	case FS_NULL:
		return "Null"
	case FS_Local:
		return "Local"
	case FS_Net:
		return "Network"
	case FS_Other:
		return "Special"
	}
	return ""
}

type FSInfo struct {
	Type       FSType
	Filesystem string
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
		cmd.Cmd = "df -B1 -P"
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

	// We are assuming it's possible for Filesystem and Mounted on to contain values with spaces.
	// So we rely on the fact that the [1-blocks, Used, Available, Capacity] columns are always right aligned.
	// By finding where the column header ends, all the values in these columns also end here.
	header := []string{"Filesystem", "1-blocks", "Used", "Available", "Capacity", "Mounted on"}
	headerEndIndex := []int{0, 0, 0, 0, 0, 0}

	for scanner.Scan() {

		line := scanner.Text()
		parts := strings.Fields(line)
		n := len(parts)

		if n == 0 {
			continue
		}

		if strings.HasPrefix(parts[0], "Filesystem") {

			for i, header := range header {

				index := strings.Index(line, header)

				if index == -1 {
					// fail immediately, invalid header
					return
				}

				// This is the distance from start of a line to the end of the header text,
				headerEndIndex[i] = index + len(header)
			}

			continue
		}

		fs := FSInfo{}

		chunk1 := line[:headerEndIndex[1]]
		chunk2 := line[headerEndIndex[1]:headerEndIndex[4]]
		chunk3 := line[headerEndIndex[4]:]

		if fsEnd := strings.LastIndex(chunk1, " "); fsEnd == -1 {
			log.Debug().Str("line", line).Msg("Parsing FS, first chunk had no space???")
			continue
		} else {
			fs.Filesystem = strings.TrimSpace(chunk1[0 : fsEnd+1])

			if strings.HasPrefix(fs.Filesystem, "//") || strings.Contains(fs.Filesystem, ":") {
				fs.Type = FS_Net
			} else if strings.HasPrefix(fs.Filesystem, "/") {
				fs.Type = FS_Local
			} else {
				fs.Type = FS_Other
			}
		}

		if usedAvailCap := strings.Fields(chunk2); len(usedAvailCap) != 3 {
			log.Debug().Str("line", line).Msg("Parsing FS, second chunk did not contain 3 parts")
			continue
		} else {
			fs.Used, _ = strconv.ParseUint(usedAvailCap[0], 10, 64)
			fs.Free, _ = strconv.ParseUint(usedAvailCap[1], 10, 64)
		}

		fs.MountPoint = strings.TrimSpace(chunk3)

		f.FSInfos = append(f.FSInfos, fs)
	}

	sort.Slice(f.FSInfos, func(i, j int) bool {

		if f.FSInfos[i].Type != f.FSInfos[j].Type {
			return f.FSInfos[i].Type < f.FSInfos[j].Type
		}

		if f.FSInfos[i].Filesystem != f.FSInfos[j].Filesystem {
			return f.FSInfos[i].Filesystem < f.FSInfos[j].Filesystem
		}

		if len(f.FSInfos[i].MountPoint) != len(f.FSInfos[j].MountPoint) {
			return len(f.FSInfos[i].MountPoint) < len(f.FSInfos[j].MountPoint)
		}

		return f.FSInfos[i].MountPoint < f.FSInfos[j].MountPoint
	})

}
