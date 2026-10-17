// Fuzz targets for the live view parsers: whatever a server prints, they
// return something or an error, never panic, and keep their caps.

package live

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzJournal(f *testing.F) {
	f.Add(`@@mitosu-who:1000 dev adm` + "\n" + `{"__CURSOR":"c1","__REALTIME_TIMESTAMP":"1792196388356155","PRIORITY":"3","MESSAGE":[104,105]}`)
	f.Add(`{"__CURSOR":"c2","__REALTIME_TIMESTAMP":"1","MESSAGE":["a","b"],"_SYSTEMD_UNIT":null}`)
	f.Fuzz(func(t *testing.T, out string) {
		res, err := Journal(context.Background(), &fake{stdout: out}, Options{}, "web", JournalQuery{Priority: -1, Lines: 50})
		if err != nil {
			return
		}
		for _, e := range res.Entries {
			if len(e.Message) > maxMessage || !utf8.ValidString(e.Message) || !utf8.ValidString(e.Unit) {
				t.Fatalf("entry over its cap or not UTF-8: %q", e.Message)
			}
		}
	})
}

func FuzzLogs(f *testing.F) {
	f.Add("2026-10-16T23:50:23.123456789Z hello\n" + exitMarker + "0\n")
	f.Add("sudo: docker: command not found\n" + exitMarker + "1\n")
	f.Fuzz(func(t *testing.T, out string) {
		res, err := Logs(context.Background(), &fake{stdout: out}, Options{}, "web", "web", 50, "")
		if err != nil {
			return
		}
		for _, l := range res.Lines {
			if len(l.Text) > maxLogLine || !utf8.ValidString(l.Text) {
				t.Fatalf("line over its cap or not UTF-8: %q", l.Text)
			}
		}
	})
}

func FuzzLineParsers(f *testing.F) {
	f.Add("Accepted publickey for dev from 172.17.0.1 port 49268 ssh2: ED25519 SHA256:x")
	f.Add("Invalid user a from 1.2.3.4 port 1 from 5.6.7.8 port 2")
	f.Add("dev : TTY=pts/0 ; PWD=/root ; USER=root ; COMMAND=/bin/sh -c a ; b")
	f.Fuzz(func(t *testing.T, msg string) {
		_, _ = parseLogin(msg)
		_, _ = parseSudo(msg)
		_, _ = parseUpgrade(msg)
		_ = seesAll(msg)
	})
}

func FuzzSystemAndInspect(f *testing.F) {
	f.Add(marker + "os\nPRETTY_NAME=\"Debian\"\n" + marker + "cpus\n4\n" + marker + "updates\na/b 1 amd64 [upgradable from: 0]\n")
	f.Add(inspectFixture)
	f.Fuzz(func(t *testing.T, out string) {
		s := parseSystem(out)
		if len(s.Updates) > maxListed || len(s.FailedUnits) > maxListed || len(s.RebootPackages) > maxListed {
			t.Fatal("list over its cap")
		}
		_, _ = parseInspect([]byte(out))
		_ = strings.TrimSpace(out)
	})
}
