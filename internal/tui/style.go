package tui

const (
	ansiReset    = "\x1b[0m"
	ansiRed      = "\x1b[31m"
	ansiGreen    = "\x1b[32m"
	ansiYellow   = "\x1b[33m"
	ansiDim      = "\x1b[2m"
	ansiBoldCyan = "\x1b[1;36m"
)

// statusColor maps a process status string to the ANSI color code that
// carries its meaning: green=running, yellow=transitional, red=alarming,
// dim=intentionally stopped. Unrecognized statuses get no color.
func statusColor(status string) string {
	switch status {
	case "RUNNING":
		return ansiGreen
	case "STARTING", "STOPPING":
		return ansiYellow
	case "FATAL", "BACKOFF":
		return ansiRed
	case "STOPPED":
		return ansiDim
	default:
		return ""
	}
}

func colorize(code, text string) string {
	if code == "" {
		return text
	}
	return code + text + ansiReset
}
