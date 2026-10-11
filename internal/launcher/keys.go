package launcher

import (
	"bytes"
	"slices"
	"unicode"
	"unicode/utf8"
)

// keyKind names one decoded keypress.
type keyKind int

const (
	keyNone      keyKind = iota // input the launcher does not use
	keyRune                     // a printable character, in key.r
	keyEnter                    // Enter
	keyBackspace                // Backspace (DEL or Ctrl-H)
	keyEsc                      // Esc on its own
	keyCtrlC                    // Ctrl-C
	keyUp                       // Up arrow, or Ctrl-P
	keyDown                     // Down arrow, or Ctrl-N
	keyPageUp                   // PgUp
	keyPageDown                 // PgDn
)

// key is one decoded keypress.
type key struct {
	kind keyKind
	r    rune // set when kind is keyRune
}

const esc = 0x1b

// keyDecoder turns raw terminal input into keys. A key can arrive split across
// reads, so an incomplete escape sequence or UTF-8 character is kept until the
// next feed. flush settles it once the caller has waited long enough.
type keyDecoder struct {
	pending []byte
}

// waiting reports whether input is held back to complete a key.
func (d *keyDecoder) waiting() bool {
	return len(d.pending) > 0
}

// feed decodes p, which follows any pending input, and returns the keys it completes.
func (d *keyDecoder) feed(p []byte) []key {
	buf := slices.Concat(d.pending, p)
	d.pending = nil
	var keys []key
	for len(buf) > 0 {
		k, n, ok := decodeKey(buf)
		if !ok {
			d.pending = bytes.Clone(buf)
			break
		}
		if k.kind != keyNone {
			keys = append(keys, k)
		}
		buf = buf[n:]
	}
	return keys
}

// flush gives up on pending input that got no continuation. A lone Esc becomes
// keyEsc; any other incomplete sequence is dropped.
func (d *keyDecoder) flush() []key {
	var keys []key
	if len(d.pending) == 1 && d.pending[0] == esc {
		keys = []key{{kind: keyEsc}}
	}
	d.pending = nil
	return keys
}

// decodeKey decodes the key at the start of buf. It returns how many bytes the
// key used, or ok=false when buf holds only the start of a key.
func decodeKey(buf []byte) (k key, n int, ok bool) {
	switch b := buf[0]; {
	case b == esc:
		return decodeEscape(buf)
	case b == '\r' || b == '\n':
		return key{kind: keyEnter}, 1, true
	case b == 0x7f || b == 0x08:
		return key{kind: keyBackspace}, 1, true
	case b == 0x03:
		return key{kind: keyCtrlC}, 1, true
	case b == 0x10:
		return key{kind: keyUp}, 1, true
	case b == 0x0e:
		return key{kind: keyDown}, 1, true
	case b < utf8.RuneSelf:
		return printable(rune(b)), 1, true
	}
	if !utf8.FullRune(buf) {
		return key{}, 0, false
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size == 1 {
		return key{}, 1, true // an invalid byte
	}
	return printable(r), size, true
}

// printable is a keyRune for a character that can be typed, and keyNone otherwise.
func printable(r rune) key {
	if unicode.IsPrint(r) {
		return key{kind: keyRune, r: r}
	}
	return key{}
}

// decodeEscape decodes a key that begins with Esc: an Esc on its own, an escape
// sequence, or Alt with a character, which the launcher ignores.
func decodeEscape(buf []byte) (key, int, bool) {
	if len(buf) == 1 {
		return key{}, 0, false // a lone Esc, or the start of a sequence
	}
	switch buf[1] {
	case esc:
		return key{kind: keyEsc}, 1, true
	case '[':
		return decodeCSI(buf)
	case 'O':
		if len(buf) < 3 {
			return key{}, 0, false
		}
		return ss3Key(buf[2]), 3, true
	}
	if !utf8.FullRune(buf[1:]) {
		return key{}, 0, false
	}
	_, size := utf8.DecodeRune(buf[1:])
	return key{}, 1 + size, true
}

// decodeCSI decodes ESC [ followed by parameter bytes and a final byte. Sequences
// the launcher does not use, such as mouse reports, decode to keyNone.
func decodeCSI(buf []byte) (key, int, bool) {
	j := 2
	for j < len(buf) && 0x20 <= buf[j] && buf[j] <= 0x3f {
		j++
	}
	switch {
	case j == len(buf):
		return key{}, 0, false
	case buf[j] < 0x40 || buf[j] > 0x7e:
		return key{}, j, true // cut short: buf[j] starts the next key
	}
	return csiKey(buf[2:j], buf[j]), j + 1, true
}

// csiKey names the key for a CSI sequence. Modifiers are ignored.
func csiKey(params []byte, final byte) key {
	switch final {
	case 'A':
		return key{kind: keyUp}
	case 'B':
		return key{kind: keyDown}
	case '~':
		first, _, _ := bytes.Cut(params, []byte(";"))
		switch string(first) {
		case "5":
			return key{kind: keyPageUp}
		case "6":
			return key{kind: keyPageDown}
		}
	}
	return key{}
}

// ss3Key names the key for ESC O final, which is how arrows arrive in
// application cursor mode.
func ss3Key(final byte) key {
	switch final {
	case 'A':
		return key{kind: keyUp}
	case 'B':
		return key{kind: keyDown}
	}
	return key{}
}
