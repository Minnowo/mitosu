package display

import (
	"fmt"
	"math"
	"strings"
)

func FmtByteU64(b uint64, align int) string {
	bf := float64(b)
	for _, unit := range []string{"B  ", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB"} {
		if math.Abs(bf) < 1024.0 {
			return fmt.Sprintf("%*.*f %s", align, 1, bf, unit)
		}
		bf /= 1024.0
	}
	return fmt.Sprintf("%*.*f Yi", align, 1, bf)
}

func FmtPercent(p float32, align int) string {
	return fmt.Sprintf("%*s%%", align, fmt.Sprintf("%.1f", p))
}

func LPad(s string, pad int) string {
	if len(s) >= pad {
		return s
	}
	return strings.Repeat(" ", pad-len(s)) + s
}
func RPad(s string, pad int) string {
	if len(s) >= pad {
		return s
	}
	return s + strings.Repeat(" ", pad-len(s))
}
