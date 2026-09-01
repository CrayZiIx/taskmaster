package tui

import "syscall"

// Terminal manages raw-mode state for a single file descriptor, saving the
// original termios so it can be restored exactly once raw mode is no longer
// needed.
type Terminal struct {
	fd    int
	orig  syscall.Termios
	isRaw bool
}

func newTerminal(fd int) *Terminal {
	return &Terminal{fd: fd}
}

// EnableRaw disables canonical mode, echo, and signal generation (ISIG) so
// that keystrokes arrive one byte at a time without kernel line-editing.
// Disabling ISIG means Ctrl-C no longer generates SIGINT at the driver level;
// it arrives as the raw byte 0x03 instead, which keys.go/shell.go handle
// explicitly (see plan notes on Ctrl-C semantics).
func (t *Terminal) EnableRaw() error {
	orig, err := getTermios(t.fd)
	if err != nil {
		return err
	}
	t.orig = orig
	t.isRaw = true
	return setTermios(t.fd, makeRaw(orig))
}

// Restore resets the terminal to whatever state it was in before EnableRaw
// was called. Safe to call multiple times or without a prior EnableRaw.
func (t *Terminal) Restore() error {
	if !t.isRaw {
		return nil
	}
	t.isRaw = false
	return setTermios(t.fd, t.orig)
}

func makeRaw(t syscall.Termios) syscall.Termios {
	raw := t
	raw.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	return raw
}
