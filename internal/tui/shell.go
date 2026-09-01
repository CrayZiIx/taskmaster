// Package tui implements a standard-library-only, line-based control shell.
// It knows nothing about processes, configuration, or job semantics: it reads
// a line, splits it into a command name and arguments, and delegates it to a
// CommandHandler.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
)

var ErrExit = errors.New("tui: exit requested")

type CommandHandler func(cmd string, args []string) (output string, err error)

type Shell struct {
	handler CommandHandler
	term    *Terminal
	editor  *LineEditor
	hist    *History
	in      *os.File
	out     io.Writer

	mu       sync.Mutex
	closed   bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

func New(handler CommandHandler) *Shell {
	return NewWithFiles(os.Stdin, os.Stdout, handler)
}

// NewWithFiles creates a shell using the supplied terminal input and output.
// It is useful for embedding the shell and for tests that provide a terminal
// compatible input file.
func NewWithFiles(in *os.File, out io.Writer, handler CommandHandler) *Shell {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	return &Shell{
		handler: handler,
		term:    newTerminal(int(in.Fd())),
		hist:    NewHistory(),
		in:      in,
		out:     out,
		stopCh:  make(chan struct{}),
	}
}

// Close restores terminal state. It is safe to call multiple times.
func (s *Shell) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	_ = s.term.Restore()
}

// Stop interrupts a running shell and restores its terminal. Closing the
// input file unblocks a pending terminal read; the caller should not reuse
// that file for another shell instance.
func (s *Shell) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		_ = s.in.Close()
	})
	s.Close()
}

func (s *Shell) Run() error { return s.RunContext(context.Background()) }

// RunContext runs the shell until EOF, an exit command, a signal, or context
// cancellation. Signal and context termination are normal shell exits; the
// supervisor remains responsible for process cleanup.
func (s *Shell) RunContext(ctx context.Context) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.term.EnableRaw(); err != nil {
		return fmt.Errorf("tui: enable raw mode: %w", err)
	}
	s.editor = NewLineEditor("taskmaster> ", s.out)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	stopWatcherDone := make(chan struct{})
	defer close(stopWatcherDone)
	go func() {
		select {
		case <-ctx.Done():
			s.Stop()
		case <-sigCh:
			s.Stop()
		case <-stopWatcherDone:
		}
	}()

	defer s.Close()
	defer func() {
		if recovered := recover(); recovered != nil {
			s.Close()
			panic(recovered)
		}
	}()

	s.editor.Reset()
	for {
		ev, readErr := ReadKey(s.readByte)
		if readErr != nil {
			if s.isStopped() || errors.Is(readErr, io.EOF) || errors.Is(readErr, os.ErrClosed) {
				fmt.Fprint(s.out, "\r\n")
				return nil
			}
			return fmt.Errorf("tui: read: %w", readErr)
		}
		exit, handlerErr := s.handleKey(ev)
		if exit {
			return handlerErr
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

func (s *Shell) isStopped() bool {
	select {
	case <-s.stopCh:
		return true
	default:
		return false
	}
}

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
	output, err := s.handler(fields[0], fields[1:])
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
