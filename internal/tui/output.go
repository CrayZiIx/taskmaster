package tui

import (
	"fmt"
	"io"
	"unicode/utf8"
)

// StatusRow is one line of a status table: a program name, its current
// status, and a free-form info string (pid/uptime/failure reason/etc).
type StatusRow struct {
	Name   string
	Status string
	Info   string
}

// FormatStatusTable renders rows as an aligned, colorized table with
// dynamically computed column widths. Widths are computed from the plain
// text first and only then wrapped in ANSI color codes — coloring before
// padding would make the %-*s width verb count invisible escape bytes and
// break alignment.
func FormatStatusTable(rows []StatusRow) string {
	nameW := utf8.RuneCountInString("NAME")
	statusW := utf8.RuneCountInString("STATUS")
	infoW := utf8.RuneCountInString("INFO")

	for _, r := range rows {
		if w := utf8.RuneCountInString(r.Name); w > nameW {
			nameW = w
		}
		if w := utf8.RuneCountInString(r.Status); w > statusW {
			statusW = w
		}
		if w := utf8.RuneCountInString(r.Info); w > infoW {
			infoW = w
		}
	}

	out := colorize(ansiBoldCyan, fmt.Sprintf("%-*s  %-*s  %-*s", nameW, "NAME", statusW, "STATUS", infoW, "INFO"))
	for _, r := range rows {
		name := colorize(ansiBoldCyan, fmt.Sprintf("%-*s", nameW, r.Name))
		status := colorize(statusColor(r.Status), fmt.Sprintf("%-*s", statusW, r.Status))
		info := fmt.Sprintf("%-*s", infoW, r.Info)
		out += "\n" + name + "  " + status + "  " + info
	}
	return out
}

// PrintPrompt writes the given prompt string, unstyled beyond what the
// caller already embedded, followed by nothing else — line editing owns
// redraws after this initial print.
func PrintPrompt(w io.Writer, prompt string) {
	fmt.Fprint(w, prompt)
}

// PrintInfo writes a plain informational line terminated with \r\n (raw
// mode has no output post-processing, so newlines are written explicitly).
func PrintInfo(w io.Writer, s string) {
	fmt.Fprint(w, s, "\r\n")
}

// PrintError writes an error line prefixed "error: " in red. Only the
// error's message is surfaced, never a Go stack trace or internal detail.
func PrintError(w io.Writer, err error) {
	fmt.Fprint(w, colorize(ansiRed, "error: "+err.Error()), "\r\n")
}
