// Storage tiers: how finely values are kept and for how long
// (docs/brief.md section 3, "Tiers").

package store

import (
	"errors"
	"fmt"
	"time"
)

// Tier keeps values at one resolution for a while. Resolution 0 is the
// raw tier (every sample); Keep 0 means forever.
type Tier struct {
	Resolution time.Duration
	Keep       time.Duration
}

// DefaultTiers are used when the config sets none: raw for 30 days,
// 1-minute buckets for a year, 1-hour buckets forever.
var DefaultTiers = []Tier{
	{Resolution: 0, Keep: 30 * 24 * time.Hour},
	{Resolution: time.Minute, Keep: 365 * 24 * time.Hour},
	{Resolution: time.Hour, Keep: 0},
}

// ValidateTiers checks a tier list: at least one tier, only the first may
// be raw, resolutions are whole seconds that grow down the list (and each
// divides the next, so buckets nest), and each tier is kept at least as
// long as the one before (forever counts as longest).
func ValidateTiers(tiers []Tier) error {
	if len(tiers) == 0 {
		return errors.New("no tiers")
	}
	for i, t := range tiers {
		if t.Keep < 0 || t.Resolution < 0 {
			return fmt.Errorf("tier %d: negative duration", i+1)
		}
		if t.Resolution%time.Second != 0 {
			return fmt.Errorf("tier %d: resolution %v is not whole seconds", i+1, t.Resolution)
		}
		if t.Resolution == 0 && i > 0 {
			return fmt.Errorf("tier %d: only the first tier may be raw", i+1)
		}
		if i == 0 {
			continue
		}
		prev := tiers[i-1]
		if t.Resolution <= prev.Resolution {
			return fmt.Errorf("tier %d: resolution %v is not coarser than %v", i+1, t.Resolution, prev.Resolution)
		}
		if prev.Resolution > 0 && t.Resolution%prev.Resolution != 0 {
			return fmt.Errorf("tier %d: resolution %v is not a multiple of %v", i+1, t.Resolution, prev.Resolution)
		}
		if (prev.Keep == 0 && t.Keep != 0) || (t.Keep != 0 && t.Keep < prev.Keep) {
			return fmt.Errorf("tier %d: kept %s, shorter than tier %d (%s)", i+1, keepString(t.Keep), i, keepString(prev.Keep))
		}
	}
	return nil
}

// keepString shows a keep duration, 0 as "forever".
func keepString(d time.Duration) string {
	if d == 0 {
		return "forever"
	}
	return d.String()
}
