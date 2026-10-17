// A container's logs (docker logs --timestamps): the newest lines, stdout
// and stderr together, and older pages before a line's timestamp.
//
// docker logs applies --tail before --until (so "--tail N --until T"
// finds nothing older), so the last lines before T are taken by tail on
// the server instead. Every line a container printed starts with docker's
// RFC 3339 timestamp; a line without one is docker's or sudo's own text,
// used only to explain a failure and never shown as a log line. The exit
// status of docker comes back on a marker line.

package live

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mitosu/src/kanshi"
)

const (
	// maxLogLine caps one log line.
	maxLogLine = 16 * 1024
	// exitMarker starts the line carrying docker's exit status.
	exitMarker = "@@mitosu-exit:"
)

// LogLine is one line a container printed.
type LogLine struct {
	TimeMs int64 `json:"t"`
	// TS is docker's own timestamp (nanoseconds), the paging cursor.
	TS   string `json:"ts"`
	Text string `json:"text"`
}

// LogsResult are a container's lines, oldest first.
type LogsResult struct {
	Lines []LogLine `json:"lines"`
	// Older is the cursor for the page before this one ("" for none).
	Older     string `json:"older"`
	Truncated bool   `json:"truncated"`
}

// Logs reads the last lines lines container printed before the timestamp
// before ("" for up to now; from LogsResult.Older).
func Logs(ctx context.Context, r Runner, o Options, host, container string, lines int, before string) (LogsResult, error) {
	if err := checkContainer(container); err != nil {
		return LogsResult{}, err
	}
	if err := checkLines(lines); err != nil {
		return LogsResult{}, err
	}
	docker := o.sudo() + "docker logs --timestamps"
	if before != "" {
		t, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			return LogsResult{}, fmt.Errorf("%w: log cursor", ErrBadInput)
		}
		// --until includes its own time: ask for up to 1 ns earlier.
		docker += " --until " + t.Add(-time.Nanosecond).UTC().Format(time.RFC3339Nano)
	} else {
		docker += " --tail " + strconv.Itoa(lines)
	}
	cmd := "{ " + docker + " -- " + quote(container) + " 2>&1; echo \"" + exitMarker + "$?\"; } | tail -n " + strconv.Itoa(lines+1)
	out, err := run(ctx, r, host, "docker", cmd, true)
	if err != nil {
		return LogsResult{}, err
	}
	res := LogsResult{Lines: []LogLine{}, Truncated: out.truncated}
	var other []string
	status := -1
	sc := bufio.NewScanner(bytes.NewReader(out.stdout))
	sc.Buffer(make([]byte, 64*1024), maxOutput)
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, exitMarker); ok {
			if n, err := strconv.Atoi(v); err == nil {
				status = n
			}
			continue
		}
		if l, ok := parseLogLine(line); ok {
			res.Lines = append(res.Lines, l)
		} else if strings.TrimSpace(line) != "" && len(other) < 5 {
			other = append(other, line)
		}
	}
	if status != 0 {
		return LogsResult{}, dockerError(status, strings.Join(other, "\n"))
	}
	if (len(res.Lines) == lines || out.truncated) && len(res.Lines) > 0 {
		res.Older = res.Lines[0].TS
	}
	return res, nil
}

// dockerError explains a failed docker logs from docker's own messages.
func dockerError(status int, text string) error {
	lower := strings.ToLower(text)
	switch {
	case status == 127 || strings.Contains(lower, "docker: command not found") || strings.Contains(lower, "docker: not found"):
		return fmt.Errorf("docker is not installed on the server, or not on the login's PATH")
	case isSudoRefusal(lower):
		return errSudoPassword("docker")
	case strings.Contains(lower, "permission denied"):
		return fmt.Errorf("this login may not use Docker: add it to group docker on the server, or set live_sudo = on")
	case status < 0:
		return fmt.Errorf("docker logs ended without its exit status (output cut off?)")
	}
	return fmt.Errorf("docker logs failed (exit %d): %s", status, kanshi.Printable(text))
}

// parseLogLine splits "2026-10-16T23:50:23.123456789Z text"; false for a
// line without docker's timestamp.
func parseLogLine(line string) (LogLine, bool) {
	ts, text, _ := strings.Cut(line, " ")
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return LogLine{}, false
	}
	if len(text) > maxLogLine {
		text = text[:maxLogLine]
	}
	return LogLine{TimeMs: t.UnixMilli(), TS: ts, Text: strings.ToValidUTF8(text, "?")}, true
}
