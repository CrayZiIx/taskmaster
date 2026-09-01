package tui

import (
	"fmt"
	"io"
	"unicode/utf8"
)

type StatusRow struct {
	Name   string
	Status string
	Info   string
}

func FormatStatusTable(rows []StatusRow) string {
	nameW := utf8.RuneCountInString("NAME")
	statusW := utf8.RuneCountInString("STATUS")
	infoW := utf8.RuneCountInString("INFO")
	for _, row := range rows {
		if width := utf8.RuneCountInString(row.Name); width > nameW {
			nameW = width
		}
		if width := utf8.RuneCountInString(row.Status); width > statusW {
			statusW = width
		}
		if width := utf8.RuneCountInString(row.Info); width > infoW {
			infoW = width
		}
	}

	out := colorize(ansiBoldCyan, fmt.Sprintf("%-*s  %-*s  %-*s", nameW, "NAME", statusW, "STATUS", infoW, "INFO"))
	for _, row := range rows {
		name := colorize(ansiBoldCyan, fmt.Sprintf("%-*s", nameW, row.Name))
		status := colorize(statusColor(row.Status), fmt.Sprintf("%-*s", statusW, row.Status))
		out += "\n" + name + "  " + status + "  " + fmt.Sprintf("%-*s", infoW, row.Info)
	}
	return out
}

func PrintPrompt(w io.Writer, prompt string) { fmt.Fprint(w, prompt) }
func PrintInfo(w io.Writer, text string)     { fmt.Fprint(w, text, "\r\n") }
func PrintError(w io.Writer, err error) {
	fmt.Fprint(w, colorize(ansiRed, "error: "+err.Error()), "\r\n")
}
