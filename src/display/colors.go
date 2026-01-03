package display

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

func Color(s string, c int) string {
	if useColor {
		return fmt.Sprintf("\x1b[%dm%v\x1b[0m", c, s)
	}
	return s
}

func ColorBold(s string, color int) string {
	if useColor {
		return fmt.Sprintf("\x1b[1;%dm%v\x1b[0m", color, s)
	}
	return s
}

// Single-color helpers
func Black(s string) string {
	return Color(s, colorBlack)
}

func Red(s string) string {
	return Color(s, colorRed)
}

func Green(s string) string {
	return Color(s, colorGreen)
}

func Yellow(s string) string {
	return Color(s, colorYellow)
}

func Blue(s string) string {
	return Color(s, colorBlue)
}

func Magenta(s string) string {
	return Color(s, colorMagenta)
}

func Cyan(s string) string {
	return Color(s, colorCyan)
}

func White(s string) string {
	return Color(s, colorWhite)
}

func DarkGray(s string) string {
	return Color(s, colorDarkGray)
}

func Bold(s string) string {
	return Color(s, colorBold)
}

func Redbold(s string) string {
	return ColorBold(s, colorRed)
}

func GreenBold(s string) string {
	return ColorBold(s, colorGreen)
}

func YellowBold(s string) string {
	return ColorBold(s, colorYellow)
}

func BlueBold(s string) string {
	return ColorBold(s, colorBlue)
}

func MagentaBold(s string) string {
	return ColorBold(s, colorMagenta)
}
