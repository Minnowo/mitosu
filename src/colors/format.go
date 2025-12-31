package colors

import (
	"fmt"
	"math"
	"strings"
)

func FmtByteU64(b uint64, align int) string {
	bf := float64(b)
	for _, unit := range []string{"  ", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "Zi"} {
		if math.Abs(bf) < 1024.0 {
			return fmt.Sprintf("%*.*f %sB", align, 1, bf, unit)
		}
		bf /= 1024.0
	}
	return fmt.Sprintf("%*.*f YiB", align, 1, bf)
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


