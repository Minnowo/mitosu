package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mitosu/src/data"
	cf "mitosu/src/display"
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
	poll := c.Value("poll").(uint)

	jsonOutput := c.Value("json").(bool)
	noPrompt := c.Value("no-prompt").(bool)
	noPassSudo := c.Value("no-pass-sudo").(bool)

	sshConfig := ssh.ExpandPath(c.Value("config").(string))
	sshAlias := c.Value("alias").(string)
	sshHost := c.Value("host").(string)
	sshPort := c.Value("port").(int)
	sshUser := c.Value("user").(string)
	sshKey := ssh.ExpandPath(c.Value("key").(string))
	sshUserPassword := c.Value("user-pass").(string)
	sshKeyPassword := c.Value("key-pass").(string)

	log.Debug().
		Uint("poll", poll).
		Bool("no-pass-sudo", noPassSudo).
		Bool("with-root", withRoot).
		Bool("json", jsonOutput).
		Bool("no-prompt", noPrompt).
		Bool("color", !noColor).
		Str("path", sshConfig).
		Str("alias", sshAlias).
		Str("host", sshHost).
		Int("port", sshPort).
		Str("user", sshUser).
		Str("key", sshKey).
		Msg("About to run stat")

	client := ssh.SSHClient{
		Config: ssh.Section{
			Name:         "mitosu CLI",
			Hostname:     sshHost,
			Port:         sshPort,
			User:         sshUser,
			IdentityFile: sshKey,
		},
		Passwords: ssh.SSHPasswords{
			KeyPassword:  sshKeyPassword,
			UserPassword: sshUserPassword,
			CanPrompt:    !noPrompt,
		},
		SudoRequiresPassword: !noPassSudo,
	}

	if sshAlias != "" {

		config, err := ssh.ParseConfig(sshConfig)

		if err != nil {
			return err
		}

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

			client.Config = section

			if err := client.Connect(); err != nil {
				return err
			} else {
				break
			}
		}
	} else if err := client.Connect(); err != nil {
		return err
	}

	if client.Client == nil {
		return fmt.Errorf("Could not get ssh connection")
	}
	defer client.Close()

	if err := client.PromptRootPass(); err != nil {
		return err
	}

	sh := shell.PosixShell{}
	allCmds := make([]shell.ShellCmd, 0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if !noColor {
		cf.SetColorEnabled(cf.SupportsANSI())
	}

	output := cf.VirtualTerm{
		FD:           int(os.Stdin.Fd()),
		SupportsAnsi: cf.SupportsANSI(),
	}

	var ticker *time.Ticker

	if poll > 0 {

		ticker = time.NewTicker(time.Duration(poll) * time.Second)
		defer ticker.Stop()

		err := output.RawMode()

		if err == nil {

			go func() {
				for {
					err = output.Input()
					if err != nil {
						stop()
						break
					}
				}
			}()
		}

		defer func() {
			output.Restore()
			PrintStats(jsonOutput, &output, systemStats)
			log.Debug().Err(err).Msg("Virtual term closed")
		}()
	}

	for {
		allCmds = allCmds[0:0]

		for _, stat := range systemStats {

			for _, cmd := range stat.GetCmds(sh.GetType()) {

				allCmds = append(allCmds, cmd)
			}
		}

		results, err := client.RunCommands(withRoot, sh, allCmds)

		if err != nil {
			return err
		}

		i := 0
		for _, stat := range systemStats {

			n := stat.CmdCount(sh.GetType())
			stat.ParseCmdOutput(sh.GetType(), results[i:i+n])
			i += n
		}

		PrintStats(jsonOutput, &output, systemStats)

		if poll <= 0 {
			break
		}

		select {
		case <-ctx.Done():
			return nil

		case <-ticker.C:
		}
	}

	return nil
}

func PrintStats(asJson bool, output *cf.VirtualTerm, stats []data.SystemStat) {

	output.Clear()

	if asJson {

		b, err := json.MarshalIndent(stats, "", "    ")

		if err != nil {

			output.Line("Error encoding json: %s", err)

		} else {

			for line := range bytes.SplitSeq(b, []byte{'\n'}) {
				output.Line("%s", string(line))
			}
		}

	} else {

		for _, stat := range stats {
			PrintStat(output, stat)
		}
	}

	output.UpdateScreenSize()
	output.Redraw()
}

func PrintStat(t *cf.VirtualTerm, stat data.SystemStat) {

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

		t.Line("")

		t.Line("%s : %s", cf.Bold(cf.LPad("Hostname", pad)), cf.GreenBold(v.Hostname))
		t.Line("%s : %s", cf.Bold(cf.LPad("Uptime", pad)), cf.Yellow(fmt.Sprintf("%dd %dh %dm %ds", d, h, m, ss)))

		t.Line("")

		t.Line("%s :  1m %s", cf.Bold(cf.LPad("Load Avg", pad)), cf.Bold(v.Load1))
		t.Line("%s :  5m %s", cf.Bold(cf.LPad("        ", pad)), cf.Bold(v.Load5))
		t.Line("%s : 10m %s", cf.Bold(cf.LPad("        ", pad)), cf.Bold(v.Load10))

		t.Line("")

		t.Line("%s : %s running of %s total", cf.Bold(cf.LPad("Processes", pad)), cf.Cyan(v.RunningProcs), cf.Cyan(v.TotalProcs))

		t.Line("")

		t.Line("%s : ", cf.MagentaBold(cf.LPad("Memory", pad)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Total", pad)), cf.Cyan(cf.FmtByteU64(v.MemTotal, memAlign)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Free", pad)), cf.Cyan(cf.FmtByteU64(v.MemFree, memAlign)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Buffers", pad)), cf.Cyan(cf.FmtByteU64(v.MemBuffers, memAlign)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Cached", pad)), cf.Cyan(cf.FmtByteU64(v.MemCached, memAlign)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Swap Total", pad)), cf.Cyan(cf.FmtByteU64(v.SwapTotal, memAlign)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Swap Free", pad)), cf.Cyan(cf.FmtByteU64(v.SwapFree, memAlign)))

		t.Line("")

		t.Line("%s : ", cf.MagentaBold(cf.LPad("CPU", pad)))
		t.Line("%s : %s", cf.Bold(cf.LPad("User", pad)), cf.Cyan(cf.FmtPercent(v.CPU.User, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Nice", pad)), cf.Cyan(cf.FmtPercent(v.CPU.Nice, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("System", pad)), cf.Cyan(cf.FmtPercent(v.CPU.System, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Idle", pad)), cf.Cyan(cf.FmtPercent(v.CPU.Idle, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("IOWait", pad)), cf.Cyan(cf.FmtPercent(v.CPU.Iowait, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("IRQ", pad)), cf.Cyan(cf.FmtPercent(v.CPU.Irq, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("SoftIRQ", pad)), cf.Cyan(cf.FmtPercent(v.CPU.SoftIrq, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Steal", pad)), cf.Cyan(cf.FmtPercent(v.CPU.Steal, cpuAlgin)))
		t.Line("%s : %s", cf.Bold(cf.LPad("Guest", pad)), cf.Cyan(cf.FmtPercent(v.CPU.Guest, cpuAlgin)))

		t.Line("")

	case *data.FSSystemStat:

		if len(v.FSInfos) < 1 {
			break
		}

		t.Line("")

		fsTypeLens := map[data.FSType]int{}
		for _, fs := range v.FSInfos {

			if fs.Filesystem[0] != '/' {
				continue
			}
			count, ok := fsTypeLens[fs.Type]

			if !ok {
				count = 0
			}
			fsTypeLens[fs.Type] = max(count, len(fs.Filesystem))
		}

		fsType := data.FS_NULL
		for _, fs := range v.FSInfos {

			if fs.Type != fsType {
				if fsType != data.FS_NULL {
					t.Line("")
				}
				fsType = fs.Type
				t.Line("%s : ", cf.MagentaBold(cf.LPad(fsType.String()+" File Systems", pad)))
			}

			t.StartLine()
			if fs.Filesystem[0] == '/' {
				// we want file paths to be right aligned
				t.Print("%s", cf.Bold(cf.LPad(cf.RPad(fs.Filesystem, fsTypeLens[fs.Type]), pad)))
			} else {
				t.Print("%s", cf.Bold(cf.LPad(fs.Filesystem, pad)))
			}

			t.Print(" : %s used  %s free  (%s)  %s",
				cf.Cyan(cf.FmtByteU64(fs.Used, fsAlign)),
				cf.Cyan(cf.FmtByteU64(fs.Free, fsAlign)),
				cf.YellowBold(cf.FmtPercent(100*(float32(fs.Used)/float32(fs.Free+fs.Used)), 4)),
				cf.Bold(fs.MountPoint),
			)
			t.FinishLine()
		}

		t.Line("")

	case *data.NetIntfSystemStat:

		if len(v.NetIntf) < 1 {
			break
		}

		t.Line("")
		t.Line("%s : ", cf.MagentaBold(cf.LPad("Network Interfaces", pad)))

		keys := make([]string, 0, len(v.NetIntf))
		for k := range v.NetIntf {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, name := range keys {

			intf := v.NetIntf[name]
			t.StartLine()
			t.Print("%s : ", cf.Bold(cf.LPad(name, pad)))
			t.Print("%s   ", cf.Yellow(cf.LPad(intf.IPv4, len("xxx.xxx.xxx.xxx/32"))))
			t.Print("%s   ", cf.Red(cf.LPad(intf.IPv6, len("xxxx:xxxx:xxxx:xxxx:xxxx:xxxx:xxxx:xxxx/64"))))
			t.Print(" in %s  ", cf.Green(cf.FmtByteU64(intf.Rx, nwAlign)))
			t.Print("out %s", cf.Green(cf.FmtByteU64(intf.Tx, nwAlign)))
			t.FinishLine()
		}
		t.Line("")

	case *data.DockerSystemStat:

		if len(v.DockerContainers) < 1 {
			break
		}

		t.Line("")
		t.Line("%s : ", cf.MagentaBold(cf.LPad("Docker Containers", pad)))

		for _, ct := range v.DockerContainers {

			t.Line("%s : CPU %s   Mem %s   NetIO %s %s   BlockIO %s %s   PIDS %s   %s ",
				cf.Bold(cf.LPad(ct.Name, pad)),
				cf.Bold(cf.LPad(ct.CPU, 6)),
				cf.Bold(cf.FmtByteU64(ct.MemUsed, 5)),
				cf.Bold(cf.FmtByteU64(ct.NetIn, 5)),
				cf.Bold(cf.FmtByteU64(ct.NetOut, 5)),
				cf.Bold(cf.FmtByteU64(ct.BlockIn, 5)),
				cf.Bold(cf.FmtByteU64(ct.BlockOut, 5)),
				cf.Bold(cf.LPad(strconv.FormatUint(ct.PIDs, 10), 4)),
				cf.Bold(ct.ID[0:len("xxxxxxxxxxxx")]),
			)
		}

		t.Line("")

	}
}
