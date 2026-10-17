// The systemd journal (journalctl -o json, systemd.journal-fields(7)):
// the newest entries, optionally of one unit, at or above a priority,
// before a time (for paging back).

package live

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxMessage caps one entry's message.
const maxMessage = 8 * 1024

// cursorPattern is a journal cursor (s=...;i=...;b=...;m=...;t=...;x=...).
var cursorPattern = regexp.MustCompile(`^[A-Za-z0-9=;_-]{1,512}$`)

// JournalQuery selects entries. Zero values mean no filter.
type JournalQuery struct {
	Unit string
	// Priority keeps entries at this priority or more urgent (0 emerg ..
	// 7 debug); -1 means all.
	Priority int
	Lines    int
	// Before keeps entries older than this journal cursor ("" for the
	// newest), from JournalResult.Older of the page shown.
	Before string
	// match are extra FIELD=value matches (used by logins and sudo).
	match []string
}

// Entry is one journal entry.
type Entry struct {
	TimeMs   int64  `json:"t"`
	Priority int    `json:"priority"`
	Unit     string `json:"unit"`
	Ident    string `json:"ident"`
	Message  string `json:"message"`
}

// JournalResult is a view's entries, oldest first.
type JournalResult struct {
	Entries []Entry `json:"entries"`
	// Older is the cursor to ask for the page before this one; "" when
	// the journal has no older matching entries.
	Older string `json:"older"`
	// Partial is set when this login only sees its own entries: not
	// root, not in group systemd-journal, adm or wheel (journalctl(1)),
	// and live_sudo off.
	Partial bool `json:"partial"`
	// Truncated is set when the output was cut at the size cap (the
	// oldest entries of the page are the ones missing).
	Truncated bool `json:"truncated"`
}

// Journal reads the newest q.Lines entries matching q on host (older than
// q.Before), newest first from journalctl so a size cap drops the oldest.
func Journal(ctx context.Context, r Runner, o Options, host string, q JournalQuery) (JournalResult, error) {
	if err := checkLines(q.Lines); err != nil {
		return JournalResult{}, err
	}
	if q.Priority < -1 || q.Priority > 7 {
		return JournalResult{}, fmt.Errorf("%w: priority must be 0 to 7", ErrBadInput)
	}
	// The first line says who reads the journal (journalctl prints no hint
	// about partial access in JSON mode). --all keeps fields over 4096
	// bytes, which are null otherwise.
	cmd := "echo \"" + whoMarker + "$(id -u) $(id -nG)\"; " + o.sudo() + "journalctl --no-pager -o json --all -r -n " + strconv.Itoa(q.Lines) +
		" --output-fields=MESSAGE,PRIORITY,_SYSTEMD_UNIT,SYSLOG_IDENTIFIER"
	if q.Unit != "" {
		if err := checkUnit(q.Unit); err != nil {
			return JournalResult{}, err
		}
		cmd += " -u " + quote(q.Unit)
	}
	if q.Priority >= 0 {
		cmd += " -p " + strconv.Itoa(q.Priority)
	}
	if q.Before != "" {
		if !cursorPattern.MatchString(q.Before) {
			return JournalResult{}, fmt.Errorf("%w: journal cursor", ErrBadInput)
		}
		// With -r, "after" the cursor is older.
		cmd += " --after-cursor=" + quote(q.Before)
	}
	for _, m := range q.match {
		cmd += " " + quote(m)
	}
	out, err := run(ctx, r, host, "journalctl", cmd)
	if err != nil {
		return JournalResult{}, err
	}
	res := JournalResult{Truncated: out.truncated, Entries: []Entry{}}
	read := 0
	sc := bufio.NewScanner(bytes.NewReader(out.stdout))
	sc.Buffer(make([]byte, 64*1024), maxOutput)
	for sc.Scan() {
		if who, ok := strings.CutPrefix(sc.Text(), whoMarker); ok {
			res.Partial = !o.Sudo && !seesAll(who)
			continue
		}
		e, cursor, ok := parseEntry(sc.Bytes())
		if cursor == "" {
			continue
		}
		read++
		res.Older = cursor
		if ok {
			res.Entries = append(res.Entries, e)
		}
	}
	// A short page is the last one.
	if read < q.Lines && !out.truncated {
		res.Older = ""
	}
	for i, j := 0, len(res.Entries)-1; i < j; i, j = i+1, j-1 {
		res.Entries[i], res.Entries[j] = res.Entries[j], res.Entries[i]
	}
	return res, nil
}

// whoMarker starts the line naming the reading login's uid and groups.
const whoMarker = "@@mitosu-who:"

// seesAll reports whether a login with "uid group group..." reads the
// whole journal: root, or a member of systemd-journal, adm or wheel.
func seesAll(who string) bool {
	f := strings.Fields(who)
	if len(f) == 0 {
		return false
	}
	if f[0] == "0" {
		return true
	}
	for _, g := range f[1:] {
		if g == "systemd-journal" || g == "adm" || g == "wheel" {
			return true
		}
	}
	return false
}

// parseEntry decodes one JSON line and returns its cursor ("" if the
// line is not an entry). A field may be a string, an array of bytes
// (non-UTF-8 or binary values) or an array of strings (a repeated field,
// joined by newlines), per journalctl(1) on -o json.
func parseEntry(line []byte) (Entry, string, bool) {
	var raw struct {
		Cursor   string          `json:"__CURSOR"`
		Time     string          `json:"__REALTIME_TIMESTAMP"`
		Priority json.RawMessage `json:"PRIORITY"`
		Unit     json.RawMessage `json:"_SYSTEMD_UNIT"`
		Ident    json.RawMessage `json:"SYSLOG_IDENTIFIER"`
		Message  json.RawMessage `json:"MESSAGE"`
	}
	if json.Unmarshal(line, &raw) != nil || !cursorPattern.MatchString(raw.Cursor) {
		return Entry{}, "", false
	}
	us, err := strconv.ParseInt(raw.Time, 10, 64)
	if err != nil || us < 0 {
		return Entry{}, raw.Cursor, false
	}
	e := Entry{TimeMs: us / 1000, Priority: 6, Unit: fieldText(raw.Unit, 256), Ident: fieldText(raw.Ident, 256)}
	if p, err := strconv.Atoi(fieldText(raw.Priority, 8)); err == nil && p >= 0 && p <= 7 {
		e.Priority = p
	}
	e.Message = fieldText(raw.Message, maxMessage)
	return e, raw.Cursor, true
}

// fieldText reads a journal JSON field as text of at most max bytes
// (valid UTF-8; "" for null or an unknown form).
func fieldText(raw json.RawMessage, max int) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var ints []int
		var many []string
		switch {
		case json.Unmarshal(raw, &ints) == nil:
			b := make([]byte, 0, len(ints))
			for _, i := range ints {
				if i >= 0 && i < 256 {
					b = append(b, byte(i))
				}
			}
			text = string(b)
		case json.Unmarshal(raw, &many) == nil:
			text = strings.Join(many, "\n")
		}
	}
	text = strings.ToValidUTF8(text, "?")
	if len(text) > max {
		text = text[:max]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return text
}
