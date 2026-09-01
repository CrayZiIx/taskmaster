package tui

import (
	"fmt"
	"io"
)

// LineEditor holds the state of the line currently being edited: its
// contents as runes and the cursor position (a rune index into buf).
//
// Simplification: cursor math assumes every rune occupies exactly one
// terminal column (no wcwidth support), so wide CJK glyphs will misalign
// the visual cursor position. Not implemented given no wcwidth in stdlib.
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

func (e *LineEditor) MoveHome() {
	e.cursor = 0
	e.redraw()
}

func (e *LineEditor) MoveEnd() {
	e.cursor = len(e.buf)
	e.redraw()
}

// KillToEnd removes everything from the cursor to the end of the line.
func (e *LineEditor) KillToEnd() {
	e.buf = e.buf[:e.cursor]
	e.redraw()
}

// KillToStart removes everything from the start of the line to the cursor.
func (e *LineEditor) KillToStart() {
	e.buf = e.buf[e.cursor:]
	e.cursor = 0
	e.redraw()
}

// SetLine replaces the buffer contents (used by history navigation) and
// places the cursor at the end.
func (e *LineEditor) SetLine(s string) {
	e.buf = []rune(s)
	e.cursor = len(e.buf)
	e.redraw()
}

// Reset clears the buffer to empty and redraws (used for a fresh prompt).
func (e *LineEditor) Reset() {
	e.clear()
	e.redraw()
}

// clear empties the buffer without redrawing, so callers can print other
// output before the fresh prompt is drawn.
func (e *LineEditor) clear() {
	e.buf = e.buf[:0]
	e.cursor = 0
}

func (e *LineEditor) String() string {
	return string(e.buf)
}

func (e *LineEditor) Len() int {
	return len(e.buf)
}

// redraw is the single place that composes the escape sequences used to
// repaint the current line: return to column 0, clear to end of line,
// rewrite prompt+buffer, then walk the cursor back left if it isn't at the
// end of the buffer.
func (e *LineEditor) redraw() {
	fmt.Fprintf(e.out, "\r\x1b[K%s%s", e.prompt, string(e.buf))
	if back := len(e.buf) - e.cursor; back > 0 {
		fmt.Fprintf(e.out, "\x1b[%dD", back)
	}
}
