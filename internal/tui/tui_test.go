package tui

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestHistoryNavigationRestoresLiveBuffer(t *testing.T) {
	history := NewHistory()
	history.Add("status")
	history.Add("start worker")
	history.Add("start worker")

	if got, ok := history.Prev("live"); !ok || got != "start worker" {
		t.Fatalf("first Prev() = %q, %v", got, ok)
	}
	if got, ok := history.Prev("ignored"); !ok || got != "status" {
		t.Fatalf("second Prev() = %q, %v", got, ok)
	}
	if got, ok := history.Next(); !ok || got != "start worker" {
		t.Fatalf("first Next() = %q, %v", got, ok)
	}
	if got, ok := history.Next(); !ok || got != "live" {
		t.Fatalf("restore Next() = %q, %v", got, ok)
	}
}

func TestReadKeyDecodesControlsEscapeAndUTF8(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want KeyEvent
	}{
		{name: "enter", data: []byte{'\n'}, want: KeyEvent{Type: KeyEnter}},
		{name: "control c", data: []byte{0x03}, want: KeyEvent{Type: KeyCtrlC}},
		{name: "arrow up", data: []byte{0x1b, '[', 'A'}, want: KeyEvent{Type: KeyUp}},
		{name: "utf8", data: []byte("é"), want: KeyEvent{Type: KeyRune, Rune: 'é'}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := 0
			readByte := func() (byte, error) {
				if index == len(tt.data) {
					return 0, io.EOF
				}
				b := tt.data[index]
				index++
				return b, nil
			}
			got, err := ReadKey(readByte)
			if err != nil || got != tt.want {
				t.Fatalf("ReadKey() = %+v, error = %v, want %+v", got, err, tt.want)
			}
		})
	}
}

func TestLineEditorEditsRunes(t *testing.T) {
	var output bytes.Buffer
	editor := NewLineEditor("> ", &output)
	editor.Insert('a')
	editor.Insert('b')
	editor.Insert('c')
	editor.MoveLeft()
	editor.DeleteBefore()
	if got := editor.String(); got != "ac" {
		t.Fatalf("after delete = %q, want ac", got)
	}
	editor.MoveHome()
	editor.Insert('x')
	if got := editor.String(); got != "xac" {
		t.Fatalf("after home insert = %q, want xac", got)
	}
	editor.KillToEnd()
	if got := editor.String(); got != "x" {
		t.Fatalf("after kill-to-end = %q, want x", got)
	}
	if output.Len() == 0 {
		t.Fatal("line editor did not redraw output")
	}
}

func TestShellSubmitsParsedCommand(t *testing.T) {
	var output bytes.Buffer
	var gotCommand string
	var gotArgs []string
	shell := &Shell{
		handler: func(command string, args []string) (string, error) {
			gotCommand = command
			gotArgs = append([]string(nil), args...)
			return "ok", nil
		},
		editor: NewLineEditor("> ", &output),
		hist:   NewHistory(),
		out:    &output,
	}
	shell.editor.SetLine("start worker")
	exit, err := shell.handleKey(KeyEvent{Type: KeyEnter})
	if exit || err != nil {
		t.Fatalf("handleKey(enter) = exit %v, error %v", exit, err)
	}
	if gotCommand != "start" || len(gotArgs) != 1 || gotArgs[0] != "worker" {
		t.Fatalf("submitted command = %q %#v", gotCommand, gotArgs)
	}
	if !strings.Contains(output.String(), "ok") {
		t.Fatalf("output = %q, want handler response", output.String())
	}

	shell.handler = func(string, []string) (string, error) { return "", ErrExit }
	shell.editor.SetLine("quit")
	exit, err = shell.handleKey(KeyEvent{Type: KeyEnter})
	if !exit || err != nil {
		t.Fatalf("exit command = exit %v, error %v", exit, err)
	}
}

func TestFormatStatusTableUsesRowsAndColors(t *testing.T) {
	table := FormatStatusTable([]StatusRow{
		{Name: "worker[1]", Status: "RUNNING", Info: "pid=42"},
		{Name: "worker[2]", Status: "FATAL", Info: "exit=7"},
	})
	for _, want := range []string{"NAME", "STATUS", "INFO", "worker[1]", "RUNNING", "pid=42", ansiGreen, ansiRed} {
		if !strings.Contains(table, want) {
			t.Fatalf("table = %q, want %q", table, want)
		}
	}
}

func TestPrintErrorKeepsErrorMessage(t *testing.T) {
	var output bytes.Buffer
	PrintError(&output, errors.New("bad command"))
	if !strings.Contains(output.String(), "error: bad command") {
		t.Fatalf("error output = %q", output.String())
	}
}
