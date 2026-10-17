// sudo and sudo-rs commands from the journal (sudoers(5), "LOG FORMAT":
// "user : TTY=pts/0 ; PWD=/home/user ; USER=root ; COMMAND=/usr/bin/true",
// with an optional reason before the first field for refusals, such as
// "3 incorrect password attempts ; ...").

package live

import (
	"context"
	"strings"
)

// SudoEntry is one logged sudo use.
type SudoEntry struct {
	TimeMs int64  `json:"t"`
	User   string `json:"user"`
	RunAs  string `json:"run_as"`
	Dir    string `json:"dir"`
	TTY    string `json:"tty"`
	// Command is the command line as sudo logged it (it may hold
	// secrets typed on the command line).
	Command string `json:"command"`
	// Problem is sudo's reason for a refusal ("" when it ran).
	Problem string `json:"problem"`
}

// SudoResult are sudo uses, oldest first.
type SudoResult struct {
	Entries []SudoEntry `json:"entries"`
	// Older is the journal cursor for the page before this one, "" for
	// none (the journal lines read, matching or not, set it).
	Older     string `json:"older"`
	Partial   bool   `json:"partial"`
	Truncated bool   `json:"truncated"`
}

// Sudo reads sudo uses from the newest lines sudo journal lines.
func Sudo(ctx context.Context, r Runner, o Options, host string, lines int, before string) (SudoResult, error) {
	j, err := Journal(ctx, r, o, host, JournalQuery{Priority: -1, Lines: lines, Before: before,
		match: []string{"_COMM=sudo", "_COMM=sudo-rs"}})
	if err != nil {
		return SudoResult{}, err
	}
	res := SudoResult{Entries: []SudoEntry{}, Older: j.Older, Partial: j.Partial, Truncated: j.Truncated}
	for _, e := range j.Entries {
		// A long command line is split into extra lines, logged as
		// "user : (command continued) ..."; join them to their command.
		if user, rest, ok := strings.Cut(e.Message, " : (command continued) "); ok {
			if n := len(res.Entries); n > 0 && res.Entries[n-1].User == strings.TrimSpace(user) {
				res.Entries[n-1].Command += rest
			}
			continue
		}
		if s, ok := parseSudo(e.Message); ok {
			s.TimeMs = e.TimeMs
			res.Entries = append(res.Entries, s)
		}
	}
	return res, nil
}

// parseSudo reads one sudo log message; false for others (such as PAM
// session lines).
func parseSudo(msg string) (SudoEntry, bool) {
	user, rest, ok := strings.Cut(msg, " : ")
	user = strings.TrimSpace(user)
	if !ok || user == "" || strings.Contains(user, " ") {
		return SudoEntry{}, false
	}
	var s SudoEntry
	s.User = user
	// COMMAND comes last and may contain " ; ".
	if i := strings.Index(rest, "COMMAND="); i >= 0 {
		s.Command = rest[i+len("COMMAND="):]
		rest = rest[:i]
	}
	for _, part := range strings.Split(rest, " ; ") {
		part = strings.TrimSpace(part)
		k, v, kv := strings.Cut(part, "=")
		switch {
		case !kv && part != "":
			s.Problem = part
		case k == "TTY":
			s.TTY = v
		case k == "PWD":
			s.Dir = v
		case k == "USER":
			s.RunAs = v
		}
	}
	if s.Command == "" && s.Problem == "" {
		return SudoEntry{}, false
	}
	return s, true
}
