package tui

import "unicode/utf8"

// KeyType identifies the kind of input event decoded from raw terminal bytes.
type KeyType int

const (
	KeyRune KeyType = iota
	KeyEnter
	KeyBackspace
	KeyCtrlC
	KeyCtrlD
	KeyCtrlA
	KeyCtrlE
	KeyCtrlK
	KeyCtrlU
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyUnknown
)

// KeyEvent is a single decoded keystroke. Rune is only meaningful when Type
// is KeyRune.
type KeyEvent struct {
	Type KeyType
	Rune rune
}

// ReadKey decodes one key event from a stream of bytes supplied one at a
// time by readByte. It fully decodes UTF-8 runes (consistent with the
// []rune buffer used by LineEditor) rather than restricting input to ASCII.
//
// Simplification: after seeing ESC (0x1b) this always blocks waiting for a
// following '[' sequence. A lone Escape keypress with no following bytes
// will therefore appear to hang until another key is pressed. Disambiguating
// a bare Escape from the start of a CSI sequence would require a short
// read-timeout on the second byte, which isn't worth the complexity here.
func ReadKey(readByte func() (byte, error)) (KeyEvent, error) {
	b, err := readByte()
	if err != nil {
		return KeyEvent{}, err
	}

	switch b {
	case '\r', '\n':
		return KeyEvent{Type: KeyEnter}, nil
	case 0x7f, 0x08:
		return KeyEvent{Type: KeyBackspace}, nil
	case 0x03:
		return KeyEvent{Type: KeyCtrlC}, nil
	case 0x04:
		return KeyEvent{Type: KeyCtrlD}, nil
	case 0x01:
		return KeyEvent{Type: KeyCtrlA}, nil
	case 0x05:
		return KeyEvent{Type: KeyCtrlE}, nil
	case 0x0b:
		return KeyEvent{Type: KeyCtrlK}, nil
	case 0x15:
		return KeyEvent{Type: KeyCtrlU}, nil
	case 0x1b:
		return readEscape(readByte)
	}

	if b < 0x20 {
		return KeyEvent{Type: KeyUnknown}, nil
	}
	if b < 0x80 {
		return KeyEvent{Type: KeyRune, Rune: rune(b)}, nil
	}

	// UTF-8 multi-byte lead byte: determine sequence length and read the
	// continuation bytes to assemble a full rune.
	n := utf8SeqLen(b)
	if n <= 1 {
		return KeyEvent{Type: KeyRune, Rune: utf8.RuneError}, nil
	}
	buf := make([]byte, n)
	buf[0] = b
	for i := 1; i < n; i++ {
		cb, err := readByte()
		if err != nil {
			return KeyEvent{}, err
		}
		buf[i] = cb
	}
	r, _ := utf8.DecodeRune(buf)
	return KeyEvent{Type: KeyRune, Rune: r}, nil
}

func utf8SeqLen(lead byte) int {
	switch {
	case lead&0xE0 == 0xC0:
		return 2
	case lead&0xF0 == 0xE0:
		return 3
	case lead&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}

func readEscape(readByte func() (byte, error)) (KeyEvent, error) {
	b, err := readByte()
	if err != nil {
		return KeyEvent{}, err
	}
	if b != '[' {
		return KeyEvent{Type: KeyUnknown}, nil
	}
	b, err = readByte()
	if err != nil {
		return KeyEvent{}, err
	}
	switch b {
	case 'A':
		return KeyEvent{Type: KeyUp}, nil
	case 'B':
		return KeyEvent{Type: KeyDown}, nil
	case 'C':
		return KeyEvent{Type: KeyRight}, nil
	case 'D':
		return KeyEvent{Type: KeyLeft}, nil
	case 'H':
		return KeyEvent{Type: KeyHome}, nil
	case 'F':
		return KeyEvent{Type: KeyEnd}, nil
	default:
		return KeyEvent{Type: KeyUnknown}, nil
	}
}
