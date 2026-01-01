package data

import (
	"bufio"
	"mitosu/src/shell"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type NetIntfInfo struct {
	IPv4 string
	IPv6 string
	Rx   uint64
	Tx   uint64
}

type NetIntfSystemStat struct {
	NetIntf map[string]NetIntfInfo
}

func (f *NetIntfSystemStat) CmdCount(sh shell.ShellType) int {
	switch sh {

	default:
		log.Panic().Int("shellType", int(sh)).Msg("Unknown shell type given")

	case shell.PosixShellType:
		return 2
	}
	return 0
}
func (f *NetIntfSystemStat) GetCmds(sh shell.ShellType) []shell.ShellCmd {

	cmds := make([]shell.ShellCmd, f.CmdCount(sh))

	switch sh {

	default:
		log.Panic().Int("shellType", int(sh)).Msg("Unknown shell type given")

	case shell.PosixShellType:

		cmds[0].Cmd = "ip -o addr"
		cmds[0].Stdin = nil

		cmds[1].Cmd = "cat /proc/net/dev"
		cmds[1].Stdin = nil
	}

	return cmds
}

func (f *NetIntfSystemStat) ParseCmdOutput(sh shell.ShellType, outs []string) {

	if len(outs) < 1 {
		log.Debug().Msg("Parsing outputs failed because it was nil or lacked enough columns")
		return
	}

	if f.NetIntf == nil {
		f.NetIntf = make(map[string]NetIntfInfo)
	} else {

		for k := range f.NetIntf {
			delete(f.NetIntf, k)
		}
	}

	{
		scanner := bufio.NewScanner(strings.NewReader(outs[0]))

		for scanner.Scan() {

			line := scanner.Text()
			parts := strings.Fields(line)

			if len(parts) >= 4 && (parts[2] == "inet" || parts[2] == "inet6") {

				ipv4 := parts[2] == "inet"
				intfname := parts[1]

				if info, ok := f.NetIntf[intfname]; ok {
					if ipv4 {
						info.IPv4 = parts[3]
					} else {
						info.IPv6 = parts[3]
					}
					f.NetIntf[intfname] = info
				} else {
					info := NetIntfInfo{}
					if ipv4 {
						info.IPv4 = parts[3]
					} else {
						info.IPv6 = parts[3]
					}
					f.NetIntf[intfname] = info
				}
			}
		}
	}

	if len(outs) < 2 {
		log.Debug().Msg("Cannot parse network interfaces information, because the outputs was truncated")
		return
	}

	{
		scanner := bufio.NewScanner(strings.NewReader(outs[1]))

		for scanner.Scan() {

			line := scanner.Text()
			parts := strings.Fields(line)

			if len(parts) != 17 {
				continue
			}
			intf := strings.TrimSpace(parts[0])
			intf = strings.TrimSuffix(intf, ":")

			if info, ok := f.NetIntf[intf]; ok {

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
				f.NetIntf[intf] = info
			}
		}
	}

}
