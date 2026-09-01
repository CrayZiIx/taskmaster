package tui

import "syscall"

type Terminal struct {
	fd    int
	orig  syscall.Termios
	isRaw bool
}

func newTerminal(fd int) *Terminal { return &Terminal{fd: fd} }

func (t *Terminal) EnableRaw() error {
	orig, err := getTermios(t.fd)
	if err != nil {
		return err
	}
	t.orig = orig
	t.isRaw = true
	return setTermios(t.fd, makeRaw(orig))
}

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
