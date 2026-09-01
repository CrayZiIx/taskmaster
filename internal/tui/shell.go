// Package tui implements a standard-library-only, line-based control shell
// (REPL) inspired by supervisorctl. It knows nothing about processes,
// configuration, or job semantics: it reads a line, splits it into a
// command name and arguments, hands them to a CommandHandler, and prints
// back whatever the handler returns. All process/daemon logic lives on the
// caller's side.
package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrExit is a sentinel a CommandHandler can return to signal that Run
// should stop the REPL cleanly (e.g. in response to a "quit" command). The
// shell package attaches no meaning to any command name itself; it only
// reacts to this sentinel coming back from the handler.
var ErrExit = errors.New("tui: exit requested")

// CommandHandler is implemented by the caller (the daemon side). It
// receives a parsed command name and its arguments and returns a string to
// print back to the user.
type CommandHandler func(cmd string, args []string) (output string, err error)

// Shell is an interactive control shell bound to a CommandHandler.
type Shell struct {
	handler CommandHandler
	term    *Terminal
	editor  *LineEditor
	hist    *History
	in      *os.File
	out     *os.File

	mu     sync.Mutex
	closed bool
}

// New creates a shell bound to the given handler. It does not touch the
// terminal until Run is called.
func New(handler CommandHandler) *Shell {
	return &Shell{
		handler: handler,
		term:    newTerminal(int(os.Stdin.Fd())),
		hist:    NewHistory(),
		in:      os.Stdin,
		out:     os.Stdout,
	}
}

// Close restores terminal state. Safe to call multiple times.
func (s *Shell) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	_ = s.term.Restore()
}

// Run puts the terminal into raw mode, prints the prompt, and blocks
// reading and dispatching lines until EOF (Ctrl-D on an empty line), the
// handler returns ErrExit, or a real external signal (e.g. SIGINT/SIGTERM
// delivered from outside this terminal) arrives. Terminal state is always
// restored on return, including on panic.
func (s *Shell) Run() (err error) {
	if err := s.term.EnableRaw(); err != nil {
		return fmt.Errorf("tui: enable raw mode: %w", err)
	}
	s.editor = NewLineEditor("taskmaster> ", s.out)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		if derr := s.in.SetReadDeadline(time.Now()); derr != nil {
			// stdin doesn't support deadlines in this environment; fall
			// back to a direct restore-and-exit so the terminal is never
			// left in raw mode.
			s.Close()
			os.Exit(130)
		}
	}()

	defer s.Close()
	defer func() {
		if r := recover(); r != nil {
			s.Close()
			panic(r)
		}
	}()

	s.editor.Reset()
	for {
		ev, rerr := ReadKey(s.readByte)
		if rerr != nil {
			if errors.Is(rerr, io.EOF) || errors.Is(rerr, os.ErrDeadlineExceeded) {
				fmt.Fprint(s.out, "\r\n")
				return nil
			}
			return fmt.Errorf("tui: read: %w", rerr)
		}
		exit, herr := s.handleKey(ev)
		if exit {
			return herr
		}
	}
}

func (s *Shell) readByte() (byte, error) {
	var b [1]byte
	n, err := s.in.Read(b[:])
	if n == 1 {
		return b[0], nil
	}
	return 0, err
}

// handleKey routes a decoded key event to the line editor/history, or
// dispatches a submitted line to the handler. The returned bool reports
// whether Run should stop.
func (s *Shell) handleKey(ev KeyEvent) (bool, error) {
	switch ev.Type {
	case KeyRune:
		s.editor.Insert(ev.Rune)
	case KeyBackspace:
		s.editor.DeleteBefore()
	case KeyLeft:
		s.editor.MoveLeft()
	case KeyRight:
		s.editor.MoveRight()
	case KeyHome, KeyCtrlA:
		s.editor.MoveHome()
	case KeyEnd, KeyCtrlE:
		s.editor.MoveEnd()
	case KeyCtrlK:
		s.editor.KillToEnd()
	case KeyCtrlU:
		s.editor.KillToStart()
	case KeyUp:
		if line, ok := s.hist.Prev(s.editor.String()); ok {
			s.editor.SetLine(line)
		}
	case KeyDown:
		if line, ok := s.hist.Next(); ok {
			s.editor.SetLine(line)
		}
	case KeyCtrlC:
		fmt.Fprint(s.out, "^C\r\n")
		s.editor.Reset()
		s.hist.ResetBrowse()
	case KeyCtrlD:
		if s.editor.Len() == 0 {
			fmt.Fprint(s.out, "\r\n")
			return true, nil
		}
	case KeyEnter:
		return s.submit()
	}
	return false, nil
}

func (s *Shell) submit() (bool, error) {
	line := strings.TrimSpace(s.editor.String())
	fmt.Fprint(s.out, "\r\n")
	s.hist.Add(line)
	s.hist.ResetBrowse()
	s.editor.clear()

	if line == "" {
		s.editor.redraw()
		return false, nil
	}

	fields := strings.Fields(line)
	cmd, args := fields[0], fields[1:]

	output, err := s.handler(cmd, args)
	if err != nil {
		if errors.Is(err, ErrExit) {
			return true, nil
		}
		PrintError(s.out, err)
		s.editor.redraw()
		return false, nil
	}
	if output != "" {
		PrintInfo(s.out, output)
	}
	s.editor.redraw()
	return false, nil
}
