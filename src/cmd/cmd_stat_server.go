package cmd

import (
	"context"
	"fmt"
	cf "mitosu/src/colors"
	"mitosu/src/data"
	"mitosu/src/ssh"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

func CmdStat(ctx context.Context, c *cli.Command) error {

	noColor := c.Value("no-color").(bool)
	cf.SetColorEnabled(!noColor)

	sshConfig := ssh.ExpandPath(c.Value("ssh-config").(string))
	sshAlias := c.Value("alias").(string)
	sshHost := c.Value("host").(string)
	sshPort := c.Value("port").(int)
	sshUser := c.Value("user").(string)
	sshKey := ssh.ExpandPath(c.Value("key").(string))

	log.Debug().
		Bool("color", !noColor). 
		Str("path", sshConfig). 
		Str("alias",sshAlias). 
		Str("host", sshHost). 
		Int("port",sshPort). 
		Str("user",sshUser). 
		Str("key",sshKey). 
		Msg("About to run stat")

	config, err := ssh.ParseConfig(sshConfig)

	if err != nil {
		return err
	}

	section := ssh.Section{
		Name:         "mitosu CLI",
		Hostname:sshHost,
		Port:         sshPort,
		User:sshUser,
		IdentityFile:sshKey,
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


	var stats data.Stats

	for range 5 {

		data.GetAllStats(client, &stats)

		PrintStats(&stats)

		time.Sleep(5 * time.Second)
	}
	return nil
}

func PrintStats(s *data.Stats) {

	pad := 25
	memAlign := 5
	cpuAlgin := 4
	fsAlign := 5
	nwAlign := 5

	d := int(s.Uptime.Hours()) / 24
	h := int(s.Uptime.Hours()) % 24
	m := int(s.Uptime.Minutes()) % 60
	ss := int(s.Uptime.Seconds()) % 60

	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Hostname",pad)), cf.CGreenBold(s.Hostname))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Uptime",pad)), cf.CYellow(fmt.Sprintf("%dd %dh %dm %ds", d, h, m,ss)))

	fmt.Println()

	fmt.Printf("%s :  1m %s\n", cf.CBold(cf.LPad("Load Avg", pad)), cf.CBold(s.Load1))
	fmt.Printf("%s :  5m %s\n", cf.CBold(cf.LPad("        ", pad)), cf.CBold(s.Load5))
	fmt.Printf("%s : 10m %s\n", cf.CBold(cf.LPad("        ", pad)), cf.CBold(s.Load10))

	fmt.Println()

	fmt.Printf("%s : %s running of %s total\n", cf.CBold(cf.LPad("Processes",pad)), cf.CCyan(s.RunningProcs), cf.CCyan(s.TotalProcs))

	fmt.Println()

	fmt.Printf("%s : \n", cf.CMagenta(cf.LPad("Memory", pad)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Total", pad)), cf.CCyan(cf.FmtByteU64(s.MemTotal, memAlign)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Free", pad)), cf.CCyan(cf.FmtByteU64(s.MemFree, memAlign)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Buffers", pad)), cf.CCyan(cf.FmtByteU64(s.MemBuffers, memAlign)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Cached", pad)), cf.CCyan(cf.FmtByteU64(s.MemCached, memAlign)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Swap Total", pad)), cf.CCyan(cf.FmtByteU64(s.SwapTotal, memAlign)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Swap Free", pad)), cf.CCyan(cf.FmtByteU64(s.SwapFree, memAlign)))

	fmt.Println()

	fmt.Printf("%s : \n",  cf.CMagenta(cf.LPad("CPU", pad)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("User",pad)), cf.CCyan(cf.FmtPercent(s.CPU.User, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Nice",pad)), cf.CCyan(cf.FmtPercent(s.CPU.Nice, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("System",pad)), cf.CCyan(cf.FmtPercent(s.CPU.System, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Idle",pad)), cf.CCyan(cf.FmtPercent(s.CPU.Idle, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("IOWait",pad)), cf.CCyan(cf.FmtPercent(s.CPU.Iowait, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("IRQ",pad)), cf.CCyan(cf.FmtPercent(s.CPU.Irq, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("SoftIRQ",pad)), cf.CCyan(cf.FmtPercent(s.CPU.SoftIrq, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Steal",pad)), cf.CCyan(cf.FmtPercent(s.CPU.Steal, cpuAlgin)))
	fmt.Printf("%s : %s\n", cf.CBold(cf.LPad("Guest",pad)), cf.CCyan(cf.FmtPercent(s.CPU.Guest, cpuAlgin)))

	fmt.Println()

	fmt.Printf("%s : \n",  cf.CMagenta(cf.LPad("File Systems", pad)))
	for _, fs := range s.FSInfos {

		fmt.Printf("%s%s : %s used   %s free   (%s)\n",
			cf.LPad("", 5),
			cf.CBold(cf.LPad(fs.MountPoint, pad)),
			cf.CGreen(cf.FmtByteU64(fs.Used, fsAlign)),
			cf.CGreen(cf.FmtByteU64(fs.Free, fsAlign)),
			cf.CYellowBold(cf.FmtPercent(100*(float32(fs.Used) / float32(fs.Free + fs.Used)), 4)),
		)
	}

	fmt.Println()

	fmt.Printf("%s : \n",  cf.CMagenta(cf.LPad("Network Interfaces", pad)))


	keys := make([]string, 0, len(s.NetIntf))
	for k := range s.NetIntf {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {

		intf := s.NetIntf[name]
		fmt.Printf("%s : ", cf.CBold(cf.LPad(name, pad)))
		fmt.Printf("%s   ", cf.CYellow(cf.LPad(intf.IPv4, len("xxx.xxx.xxx.xxx/32"))))
		fmt.Printf("%s   ", cf.CRed(cf.LPad(intf.IPv6, len("xxxx:xxxx:xxxx:xxxx:xxxx:xxxx:xxxx:xxxx/64"))))
		fmt.Printf(" in %s  ", cf.CGreen(cf.FmtByteU64(intf.Rx, nwAlign)))
		fmt.Printf("out %s\n", cf.CGreen(cf.FmtByteU64(intf.Tx, nwAlign)))
	}

	fmt.Println()
}
