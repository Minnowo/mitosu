package colors

import "fmt"

const (
	colorBlack = iota + 30
	colorRed
	colorGreen
	colorYellow
	colorBlue
	colorMagenta
	colorCyan
	colorWhite

	colorBold     = 1
	colorDarkGray = 90
)

var useColor = true

func SetColorEnabled(enable bool) {
	useColor = enable
}

func WithColor(s string, c int) string {
	if useColor {
		return fmt.Sprintf("\x1b[%dm%v\x1b[0m", c, s)
	}
	return s
}

func WithColorBold(s string, color int) string {
	if useColor {
		return fmt.Sprintf("\x1b[1;%dm%v\x1b[0m", color, s)
	}
	return s
}

// Single-color helpers
func CBlack(s string) string {
	return WithColor(s, colorBlack)
}

func CRed(s string) string {
	return WithColor(s, colorRed)
}

func CGreen(s string) string {
	return WithColor(s, colorGreen)
}

func CYellow(s string) string {
	return WithColor(s, colorYellow)
}

func CBlue(s string) string {
	return WithColor(s, colorBlue)
}

func CMagenta(s string) string {
	return WithColor(s, colorMagenta)
}

func CCyan(s string) string {
	return WithColor(s, colorCyan)
}

func CWhite(s string) string {
	return WithColor(s, colorWhite)
}

func CDarkGray(s string) string {
	return WithColor(s, colorDarkGray)
}

func CBold(s string) string {
	return WithColor(s, colorBold)
}

func CRedBold(s string) string {
	return WithColorBold(s, colorRed)
}

func CGreenBold(s string) string {
	return WithColorBold(s, colorGreen)
}

func CYellowBold(s string) string {
	return WithColorBold(s, colorYellow)
}

func CBlueBold(s string) string {
	return WithColorBold(s, colorBlue)
}
