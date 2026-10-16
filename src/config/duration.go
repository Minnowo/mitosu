// Durations in mitosu.conf: a whole number and one unit, s, m, h, d
// (24 h), w (7 d) or y (365 d), such as 30s, 5m, 180d, 2y.

package config

import (
	"fmt"
	"strconv"
	"time"
)

// maxDurationYears keeps durations far below time.Duration's limit.
const maxDurationYears = 100

// parseDuration parses a duration as described in the file header.
func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("duration %q: want a number and a unit (s, m, h, d, w, y)", s)
	}
	units := map[byte]time.Duration{
		's': time.Second,
		'm': time.Minute,
		'h': time.Hour,
		'd': 24 * time.Hour,
		'w': 7 * 24 * time.Hour,
		'y': 365 * 24 * time.Hour,
	}
	unit, ok := units[s[len(s)-1]]
	if !ok {
		return 0, fmt.Errorf("duration %q: unknown unit (s, m, h, d, w, y)", s)
	}
	digits := s[:len(s)-1]
	if digits[0] == '0' && len(digits) > 1 {
		return 0, fmt.Errorf("duration %q: leading zero", s)
	}
	n, err := strconv.ParseUint(digits, 10, 32)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("duration %q: want a whole number above 0", s)
	}
	d := time.Duration(n) * unit
	if d > maxDurationYears*365*24*time.Hour {
		return 0, fmt.Errorf("duration %q: more than %d years", s, maxDurationYears)
	}
	return d, nil
}
