// SSH logins from the journal: sshd (and sshd-session, OpenSSH 9.8+)
// lines for accepted and failed logins (sshd(8) log formats, as seen on
// Debian 13: "Accepted publickey for dev from 172.17.0.1 port 49268 ssh2:
// ...", "Failed password for invalid user x from ...", "Invalid user x
// from ...").

package live

import (
	"context"
	"regexp"
)

// Login is one login attempt.
type Login struct {
	TimeMs int64 `json:"t"`
	// Result is accepted, failed or invalid_user.
	Result string `json:"result"`
	User   string `json:"user"`
	From   string `json:"from"`
	// Method is publickey, password, keyboard-interactive, ... ("" if
	// the line does not say).
	Method string `json:"method"`
}

// LoginsResult are login attempts, oldest first.
type LoginsResult struct {
	Logins []Login `json:"logins"`
	// Older is the journal cursor for the page before this one, "" for
	// none (the journal lines read, matching or not, set it).
	Older     string `json:"older"`
	Partial   bool   `json:"partial"`
	Truncated bool   `json:"truncated"`
}

// The user name in failed and invalid-user lines is chosen by the client
// and may hold spaces, even " from X port N": the user part is greedy and
// the address is the last " from <addr> port <n>" before the line's fixed
// ending, so it cannot be faked.
var (
	acceptedLine = regexp.MustCompile(`^Accepted (\S+) for (\S+) from (\S+) port \d+ ssh2(?::|$)`)
	failedLine   = regexp.MustCompile(`^Failed (\S+) for (?:invalid user )?(.*) from (\S+) port \d+ ssh2$`)
	invalidLine  = regexp.MustCompile(`^Invalid user (.*) from (\S+) port \d+$`)
)

// Logins reads login attempts from the newest lines sshd journal lines.
func Logins(ctx context.Context, r Runner, o Options, host string, lines int, before string) (LoginsResult, error) {
	j, err := Journal(ctx, r, o, host, JournalQuery{Priority: -1, Lines: lines, Before: before,
		match: []string{"_COMM=sshd", "_COMM=sshd-session"}})
	if err != nil {
		return LoginsResult{}, err
	}
	res := LoginsResult{Logins: []Login{}, Older: j.Older, Partial: j.Partial, Truncated: j.Truncated}
	for _, e := range j.Entries {
		if l, ok := parseLogin(e.Message); ok {
			l.TimeMs = e.TimeMs
			res.Logins = append(res.Logins, l)
		}
	}
	return res, nil
}

// parseLogin reads one sshd message; false for other messages.
func parseLogin(msg string) (Login, bool) {
	if m := acceptedLine.FindStringSubmatch(msg); m != nil {
		return Login{Result: "accepted", Method: m[1], User: m[2], From: m[3]}, true
	}
	if m := failedLine.FindStringSubmatch(msg); m != nil {
		return Login{Result: "failed", Method: m[1], User: m[2], From: m[3]}, true
	}
	if m := invalidLine.FindStringSubmatch(msg); m != nil {
		return Login{Result: "invalid_user", User: m[1], From: m[2]}, true
	}
	return Login{}, false
}
