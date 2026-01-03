package display

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"unicode/utf8"

	"golang.org/x/term"
)

var (
	// https://en.wikipedia.org/wiki/ANSI_escape_code
	ansiEnableAltBuf  = "\033[?1049h"
	ansiDisableAltBuf = "\033[?1049l"
	ansiEraseDisplay2 = "\033[2J"
	ansiCursorPos     = "\033[H"

	ansiClearReset = ansiEraseDisplay2 + ansiCursorPos
)

var (
	// https://en.wikipedia.org/wiki/ANSI_escape_code#Terminal_input_sequences
	// ANSI escape codes
	esc = byte(27)

	// Navigation keys
	KeyUp    = []byte{esc, '[', 'A'}
	KeyDown  = []byte{esc, '[', 'B'}
	KeyRight = []byte{esc, '[', 'C'}
	KeyLeft  = []byte{esc, '[', 'D'}

	KeyUpAlt    = []byte{'k'}
	KeyDownAlt  = []byte{'j'}
	KeyRightAlt = []byte{'l'}
	KeyLeftAlt  = []byte{'h'}

	KeyUpAlt2    = []byte{'K'}
	KeyDownAlt2  = []byte{'J'}
	KeyRightAlt2 = []byte{'L'}
	KeyLeftAlt2  = []byte{'H'}

	// Quit keys
	CtrlC = []byte{3}
	CtrlD = []byte{4}
	CtrlZ = []byte{26}
	Quit1 = []byte{'q'}
	Quit2 = []byte{'Q'}
)

var (
	ErrNoAnsiSupport = errors.New("Ansi support is disabled, will not use raw mode")
	ErrUserQuit      = errors.New("Abort, Ctrl+C")
)

type VirtualTerm struct {
	FD           int
	YOffset      int
	XOffset      int
	Width        int
	Height       int
	UserMove     bool
	Lines        []string
	SupportsAnsi bool

	lineBuffer  bytes.Buffer
	inputBuffer [32]byte
	truncBuffer []rune
	state       *term.State
}

func (p *VirtualTerm) IsRaw() bool {
	return p.state != nil
}

func (p *VirtualTerm) RawMode() error {

	if !p.SupportsAnsi {
		return ErrNoAnsiSupport
	}

	if p.IsRaw() {
		p.Restore()
	}

	fmt.Print(ansiEnableAltBuf)

	if state, err := term.MakeRaw(p.FD); err != nil {

		fmt.Print(ansiDisableAltBuf)

		return err
	} else {
		p.state = state
	}
	return nil
}

func (p *VirtualTerm) Restore() {

	if !p.SupportsAnsi {
		return
	}

	fmt.Print(ansiDisableAltBuf)

	if p.state != nil {
		term.Restore(p.FD, p.state)
		p.state = nil
	}
}

func (p *VirtualTerm) UpdateScreenSize() {
	if w, h, err := term.GetSize(p.FD); err == nil {
		p.Width = w
		p.Height = h
	}
}

func (p *VirtualTerm) Clear() {
	p.Lines = p.Lines[0:0]
}

func (p *VirtualTerm) StartLine() {
	p.lineBuffer.Reset()
}

func (p *VirtualTerm) Print(format string, args ...any) {

	line := fmt.Sprintf(format, args...)

	p.lineBuffer.Write([]byte(line))
}

func (p *VirtualTerm) FinishLine() {

	p.Lines = append(p.Lines, p.lineBuffer.String())
	p.lineBuffer.Reset()
}

func (p *VirtualTerm) Line(format string, args ...any) {

	line := fmt.Sprintf(format, args...)

	p.Lines = append(p.Lines, line)
}

func (p *VirtualTerm) ScrollHeight() int {
	return p.Height - 2
}

func (p *VirtualTerm) Redraw() {

	if !p.IsRaw() {
		// Regular printing

		for _, line := range p.Lines {

			fmt.Println(line)
		}

	} else {
		// Virtual scrolling

		if p.SupportsAnsi {
			fmt.Print(ansiClearReset)
		}

		if !p.UserMove && len(p.Lines) > p.ScrollHeight() {
			p.YOffset = len(p.Lines) - p.ScrollHeight()
		}

		for i := 0; i < p.ScrollHeight(); i++ {

			idx := p.YOffset + i

			if idx >= len(p.Lines) {
				break
			}

			line := p.Lines[idx]
			fmt.Print(p.truncateANSI(line, p.Width-1))
			fmt.Print("\r\n")
		}
		fmt.Print("q to quit, arrow keys or hjkl to navigate\r\n")
	}
}

func (p *VirtualTerm) Input() error {

	n, err := os.Stdin.Read(p.inputBuffer[:])

	if err != nil {
		return err
	}

	buf := p.inputBuffer[0:n]

	switch {

	case
		bytes.Equal(buf, Quit1),
		bytes.Equal(buf, Quit2),
		bytes.Equal(buf, CtrlC),
		bytes.Equal(buf, CtrlD),
		bytes.Equal(buf, CtrlZ):

		return ErrUserQuit

	case
		bytes.Equal(buf, KeyLeft),
		bytes.Equal(buf, KeyLeftAlt),
		bytes.Equal(buf, KeyLeftAlt2):

		if p.XOffset-1 >= 0 {
			p.XOffset--
			p.Redraw()
		}

	case
		bytes.Equal(buf, KeyRight),
		bytes.Equal(buf, KeyRightAlt),
		bytes.Equal(buf, KeyRightAlt2):

		if p.XOffset+1 >= 0 {
			p.XOffset++
			p.Redraw()
		}

	case
		bytes.Equal(buf, KeyUp),
		bytes.Equal(buf, KeyUpAlt),
		bytes.Equal(buf, KeyUpAlt2):

		if p.YOffset-1 >= 0 {
			p.UserMove = true
			p.YOffset--
			p.Redraw()
		}

	case
		bytes.Equal(buf, KeyDown),
		bytes.Equal(buf, KeyDownAlt),
		bytes.Equal(buf, KeyDownAlt2):

		limit := len(p.Lines) - p.ScrollHeight()

		if p.YOffset+1 <= limit {
			p.UserMove = p.YOffset+1 != limit
			p.YOffset++
			p.Redraw()
		}
	}

	return nil
}

func (p *VirtualTerm) truncateANSI(s string, maxWidth int) string {

	width := 0
	trunc := p.XOffset

	if trunc == 0 && len(s) < maxWidth {
		return s
	}

	if p.truncBuffer != nil {
		p.truncBuffer = make([]rune, 0, len(s))
	} else {
		p.truncBuffer = p.truncBuffer[0:0]
	}

	for i := 0; i < len(s); {

		// ANSI escape sequence
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++ // include 'm'
			}
			p.truncBuffer = append(p.truncBuffer, []rune(s[i:j])...)
			i = j
			continue
		}

		// Decode rune
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}

		if width < maxWidth {
			if trunc > 0 {
				trunc--
			} else {
				p.truncBuffer = append(p.truncBuffer, r)
				width++
			}
		} else {
			break
		}

		i += size
	}

	return string(p.truncBuffer)
}
