package tui

import (
	"fmt"
	"io"
)

type LineEditor struct {
	buf    []rune
	cursor int
	prompt string
	out    io.Writer
}

func NewLineEditor(prompt string, out io.Writer) *LineEditor {
	return &LineEditor{prompt: prompt, out: out}
}

func (e *LineEditor) Insert(r rune) {
	e.buf = append(e.buf[:e.cursor], append([]rune{r}, e.buf[e.cursor:]...)...)
	e.cursor++
	e.redraw()
}

func (e *LineEditor) DeleteBefore() {
	if e.cursor == 0 {
		return
	}
	e.buf = append(e.buf[:e.cursor-1], e.buf[e.cursor:]...)
	e.cursor--
	e.redraw()
}

func (e *LineEditor) MoveLeft() {
	if e.cursor > 0 {
		e.cursor--
	}
	e.redraw()
}

func (e *LineEditor) MoveRight() {
	if e.cursor < len(e.buf) {
		e.cursor++
	}
	e.redraw()
}

func (e *LineEditor) MoveHome() { e.cursor = 0; e.redraw() }
func (e *LineEditor) MoveEnd()  { e.cursor = len(e.buf); e.redraw() }

func (e *LineEditor) KillToEnd() {
	e.buf = e.buf[:e.cursor]
	e.redraw()
}

func (e *LineEditor) KillToStart() {
	e.buf = e.buf[e.cursor:]
	e.cursor = 0
	e.redraw()
}

func (e *LineEditor) SetLine(s string) {
	e.buf = []rune(s)
	e.cursor = len(e.buf)
	e.redraw()
}

func (e *LineEditor) Reset() {
	e.clear()
	e.redraw()
}

func (e *LineEditor) clear() {
	e.buf = e.buf[:0]
	e.cursor = 0
}

func (e *LineEditor) String() string { return string(e.buf) }
func (e *LineEditor) Len() int       { return len(e.buf) }

func (e *LineEditor) redraw() {
	fmt.Fprintf(e.out, "\r\x1b[K%s%s", e.prompt, string(e.buf))
	if back := len(e.buf) - e.cursor; back > 0 {
		fmt.Fprintf(e.out, "\x1b[%dD", back)
	}
}
