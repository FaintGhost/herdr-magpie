package launcher

import (
	"slices"
	"testing"
)

func TestKeyDecoder(t *testing.T) {
	ch := func(r rune) key { return key{kind: keyRune, r: r} }
	kind := func(k keyKind) key { return key{kind: k} }
	tests := []struct {
		name   string
		chunks []string // fed one at a time, as reads from the terminal arrive
		flush  bool     // flush after the last chunk, as the Esc timer does
		want   []key
	}{
		{name: "printable ASCII", chunks: []string{"ab /"}, want: []key{ch('a'), ch('b'), ch(' '), ch('/')}},
		{name: "UTF-8 characters", chunks: []string{"中文"}, want: []key{ch('中'), ch('文')}},
		{name: "UTF-8 split across reads", chunks: []string{"\xe4", "\xb8\xad"}, want: []key{ch('中')}},
		{name: "invalid byte is ignored", chunks: []string{"\xff"}},
		{name: "control characters are ignored", chunks: []string{"\x01\x1e"}},
		{name: "enter as CR and LF", chunks: []string{"\r\n"}, want: []key{kind(keyEnter), kind(keyEnter)}},
		{name: "backspace as DEL and Ctrl-H", chunks: []string{"\x7f\x08"}, want: []key{kind(keyBackspace), kind(keyBackspace)}},
		{name: "ctrl-c", chunks: []string{"\x03"}, want: []key{kind(keyCtrlC)}},
		{name: "ctrl-p and ctrl-n move", chunks: []string{"\x10\x0e"}, want: []key{kind(keyUp), kind(keyDown)}},
		{name: "arrows", chunks: []string{"\x1b[A\x1b[B"}, want: []key{kind(keyUp), kind(keyDown)}},
		{name: "arrows in application cursor mode", chunks: []string{"\x1bOA\x1bOB"}, want: []key{kind(keyUp), kind(keyDown)}},
		{name: "page keys", chunks: []string{"\x1b[5~\x1b[6~"}, want: []key{kind(keyPageUp), kind(keyPageDown)}},
		{name: "modified arrow", chunks: []string{"\x1b[1;5A"}, want: []key{kind(keyUp)}},
		{name: "unused keys are ignored", chunks: []string{"\x1b[H\x1b[F\x1bOP"}},
		{name: "mouse report is ignored", chunks: []string{"\x1b[<0;10;5M"}},
		{name: "sequence split after ESC", chunks: []string{"\x1b", "[B"}, want: []key{kind(keyDown)}},
		{name: "sequence split after bracket", chunks: []string{"\x1b[", "A"}, want: []key{kind(keyUp)}},
		{name: "SS3 split after O", chunks: []string{"\x1bO", "B"}, want: []key{kind(keyDown)}},
		{name: "lone Esc needs flush", chunks: []string{"\x1b"}, flush: true, want: []key{kind(keyEsc)}},
		{name: "Esc then Esc", chunks: []string{"\x1b\x1b"}, flush: true, want: []key{kind(keyEsc), kind(keyEsc)}},
		{name: "Esc then Up in one read", chunks: []string{"\x1b\x1b[A"}, want: []key{kind(keyEsc), kind(keyUp)}},
		{name: "Alt+key is ignored", chunks: []string{"\x1bx"}},
		{name: "Esc then letter in separate reads is Alt+key", chunks: []string{"\x1b", "x"}},
		{name: "incomplete sequence is dropped on flush", chunks: []string{"\x1b[1"}, flush: true},
		{name: "incomplete UTF-8 is dropped on flush", chunks: []string{"\xe4"}, flush: true},
		{name: "mixed input", chunks: []string{"a\x1b[Bb中"}, want: []key{ch('a'), kind(keyDown), ch('b'), ch('中')}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d keyDecoder
			var got []key
			for _, c := range tc.chunks {
				got = append(got, d.feed([]byte(c))...)
			}
			if tc.flush {
				got = append(got, d.flush()...)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKeyDecoderWaitsForRestOfKey(t *testing.T) {
	var d keyDecoder
	if keys := d.feed([]byte("\x1b")); len(keys) != 0 || !d.waiting() {
		t.Fatalf("after a lone Esc: keys = %v, waiting = %v; want no keys and waiting", keys, d.waiting())
	}
	if keys := d.feed([]byte("[")); len(keys) != 0 || !d.waiting() {
		t.Fatalf("after ESC [: keys = %v, waiting = %v; want no keys and waiting", keys, d.waiting())
	}
	if keys := d.feed([]byte("A")); !slices.Equal(keys, []key{{kind: keyUp}}) || d.waiting() {
		t.Fatalf("after ESC [ A: keys = %v, waiting = %v; want Up and not waiting", keys, d.waiting())
	}
	if keys := d.feed([]byte("\x1b")); len(keys) != 0 {
		t.Fatalf("lone Esc produced keys %v before flush", keys)
	}
	if keys := d.flush(); !slices.Equal(keys, []key{{kind: keyEsc}}) || d.waiting() {
		t.Fatalf("flush after lone Esc: keys = %v, waiting = %v; want Esc and not waiting", keys, d.waiting())
	}
}
