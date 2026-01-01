package cmd

import (
	"context"
	"fmt"
	cf "mitosu/src/colors"
	"mitosu/src/data"
	"mitosu/src/shell"
	"mitosu/src/ssh"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func CmdStat(ctx context.Context, c *cli.Command, systemStats []data.SystemStat) error {

	noColor := c.Value("no-color").(bool)
	cf.SetColorEnabled(!noColor)

	withRoot := c.Value("with-root").(bool)
	poll := c.Value("poll").(bool)

	sshConfig := ssh.ExpandPath(c.Value("ssh-config").(string))
	sshAlias := c.Value("alias").(string)
	sshHost := c.Value("host").(string)
	sshPort := c.Value("port").(int)
	sshUser := c.Value("user").(string)
	sshKey := ssh.ExpandPath(c.Value("key").(string))

	log.Debug().
		Bool("poll", poll).
		Bool("with-root", withRoot).
		Bool("color", !noColor).
		Str("path", sshConfig).
		Str("alias", sshAlias).
		Str("host", sshHost).
		Int("port", sshPort).
		Str("user", sshUser).
		Str("key", sshKey).
		Msg("About to run stat")

	config, err := ssh.ParseConfig(sshConfig)

	if err != nil {
		return err
	}

	section := ssh.Section{
		Name:         "mitosu CLI",
		Hostname:     sshHost,
		Port:         sshPort,
		User:         sshUser,
		IdentityFile: sshKey,
	}

	var client ssh.SSHClient

	if sshAlias != "" {

		for _, section := range config.Sections {

			if sshAlias != section.Name {
				continue
			}

			log.Debug().
				Str("alias", sshAlias).
				Str("user", section.User).
				Str("host", section.Hostname).
				Int("port", section.Port).
				Str("key", section.IdentityFile).
				Msg("Found ssh host alias")

			if c, err := ssh.NewClient(section); err != nil {
				return err
			} else {
				client = c
				break
			}
		}
	} else {

		if c, err := ssh.NewClient(section); err != nil {
			return err
		} else {
			client = c
		}
	}

	if client.Client == nil {
		return fmt.Errorf("Could not get ssh connection")
	}

	var sh shell.Shell

	if withRoot {

		pd, err := ssh.PromptForPasswordF(
			"Enter the sudo password for %s@%s: ",
			client.Config.Hostname, client.Config.User,
		)

		if err != nil {
			return err
		}

		sh = shell.NewPosixShell(string(pd))
	} else {

		sh = shell.NewPosixShell("")
	}

	shellType := sh.GetType()
	allCmds := make([]shell.ShellCmd, 0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	defer client.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		allCmds = allCmds[0:0]

		for _, stat := range systemStats {

			for _, cmd := range stat.GetCmds(shellType) {

				allCmds = append(allCmds, cmd)
			}
		}

		results, err := client.RunCommands(sh, allCmds)

		if err != nil {
			return err
		}

		i := 0
		for _, stat := range systemStats {

			n := stat.CmdCount(shellType)
			stat.ParseCmdOutput(shellType, results[i:i+n])
			i += n

			PrintStat(stat)
		}

		if !poll {
			break
		}

		select {
		case <-ctx.Done():
			fmt.Println("\nExiting cleanly.")
			return nil
		case <-ticker.C:
			fmt.Print("\033[H\033[2J")
		}
	}

	return nil
}

func PrintStat(stat data.SystemStat) {

	pad := 30
	memAlign := 5
	cpuAlgin := 4
	fsAlign := 5
	nwAlign := 5

	switch v := stat.(type) {

	case *data.ProcInfoSystemStat:
		d := int(v.Uptime.Hours()) / 24
		h := int(v.Uptime.Hours()) % 24
		m := int(v.Uptime.Minutes()) % 60
		ss := int(v.Uptime.Seconds()) % 60

		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Hostname", pad)), cf.CGreenBold(v.Hostname))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Uptime", pad)), cf.CYellow(fmt.Sprintf("%dd %dh %dm %ds", d, h, m, ss)))

		fmt.Println()

		fmt.Printf("%s :  1m %s\n", cf.CBold(cf.LPad("Load Avg", pad)), cf.CBold(v.Load1))
		fmt.Printf("%s :  5m %s\n", cf.CBold(cf.LPad("        ", pad)), cf.CBold(v.Load5))
		fmt.Printf("%s : 10m %s\n", cf.CBold(cf.LPad("        ", pad)), cf.CBold(v.Load10))

		fmt.Println()

		fmt.Printf("%s : %s running of %s total\n", cf.CBold(cf.LPad("Processes", pad)), cf.CCyan(v.RunningProcs), cf.CCyan(v.TotalProcs))

		fmt.Println()

		fmt.Printf("%s : \n", cf.CMagenta(cf.LPad("Memory", pad)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Total", pad)), cf.CCyan(cf.FmtByteU64(v.MemTotal, memAlign)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Free", pad)), cf.CCyan(cf.FmtByteU64(v.MemFree, memAlign)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Buffers", pad)), cf.CCyan(cf.FmtByteU64(v.MemBuffers, memAlign)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Cached", pad)), cf.CCyan(cf.FmtByteU64(v.MemCached, memAlign)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Swap Total", pad)), cf.CCyan(cf.FmtByteU64(v.SwapTotal, memAlign)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Swap Free", pad)), cf.CCyan(cf.FmtByteU64(v.SwapFree, memAlign)))

		fmt.Println()

		fmt.Printf("%s : \n", cf.CMagenta(cf.LPad("CPU", pad)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("User", pad)), cf.CCyan(cf.FmtPercent(v.CPU.User, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Nice", pad)), cf.CCyan(cf.FmtPercent(v.CPU.Nice, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("System", pad)), cf.CCyan(cf.FmtPercent(v.CPU.System, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Idle", pad)), cf.CCyan(cf.FmtPercent(v.CPU.Idle, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("IOWait", pad)), cf.CCyan(cf.FmtPercent(v.CPU.Iowait, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("IRQ", pad)), cf.CCyan(cf.FmtPercent(v.CPU.Irq, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("SoftIRQ", pad)), cf.CCyan(cf.FmtPercent(v.CPU.SoftIrq, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Steal", pad)), cf.CCyan(cf.FmtPercent(v.CPU.Steal, cpuAlgin)))
		fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Guest", pad)), cf.CCyan(cf.FmtPercent(v.CPU.Guest, cpuAlgin)))

		fmt.Println()

	case *data.FSSystemStat:

		fmt.Printf("%s : \n", cf.CMagenta(cf.LPad("File Systems", pad)))
		for _, fs := range v.FSInfos {

			fmt.Printf("%s : %s used   %s free   (%s)\n",
				cf.CBold(cf.LPad(fs.MountPoint, pad)),
				cf.CGreen(cf.FmtByteU64(fs.Used, fsAlign)),
				cf.CGreen(cf.FmtByteU64(fs.Free, fsAlign)),
				cf.CYellowBold(cf.FmtPercent(100*(float32(fs.Used)/float32(fs.Free+fs.Used)), 4)),
			)
		}

		fmt.Println()

	case *data.NetIntfSystemStat:

		fmt.Printf("%s : \n", cf.CMagenta(cf.LPad("Network Interfaces", pad)))

		keys := make([]string, 0, len(v.NetIntf))
		for k := range v.NetIntf {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, name := range keys {

			intf := v.NetIntf[name]
			fmt.Printf("%s : ", cf.CBold(cf.LPad(name, pad)))
			fmt.Printf("%s   ", cf.CYellow(cf.LPad(intf.IPv4, len("xxx.xxx.xxx.xxx/32"))))
			fmt.Printf("%s   ", cf.CRed(cf.LPad(intf.IPv6, len("xxxx:xxxx:xxxx:xxxx:xxxx:xxxx:xxxx:xxxx/64"))))
			fmt.Printf(" in %s  ", cf.CGreen(cf.FmtByteU64(intf.Rx, nwAlign)))
			fmt.Printf("out %s\n", cf.CGreen(cf.FmtByteU64(intf.Tx, nwAlign)))
		}

	case *data.DockerSystemStat:

		fmt.Printf("%s : \n", cf.CMagenta(cf.LPad("Docker containers", pad)))
		for _, ct := range v.DockerContainers {

			fmt.Printf("%s : CPU %s   Mem %s   NetIO %s %s   BlockIO %s %s   PIDS %s   %s \n",
				cf.CBold(cf.LPad(ct.Name, pad)),
				cf.CBold(cf.LPad(ct.CPU, 6)),
				cf.CBold(cf.FmtByteU64(ct.MemUsed, 5)),
				cf.CBold(cf.FmtByteU64(ct.NetIn, 5)),
				cf.CBold(cf.FmtByteU64(ct.NetOut, 5)),
				cf.CBold(cf.FmtByteU64(ct.BlockIn, 5)),
				cf.CBold(cf.FmtByteU64(ct.BlockOut, 5)),
				cf.CBold(cf.LPad(strconv.FormatUint(ct.PIDs, 10), 4)),
				cf.CBold(ct.ID[0:len("xxxxxxxxxxxx")]),
			)
		}

		fmt.Println()

	}
}
