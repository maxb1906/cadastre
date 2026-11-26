package wtf8

import (
	"slices"
	"testing"
	"unicode/utf16"
)

func TestRoundTrip(t *testing.T) {
	cases := [][]uint16{
		utf16.Encode([]rune("hello.txt")),
		utf16.Encode([]rune("Évènement – 日本語 😀")),
		{0x0041, 0xD800, 0x0042},         // lone high surrogate
		{0xDC00, 0x0041},                 // lone low surrogate
		{0xD83D, 0xDE00, 0xD83D},         // pair then lone high at end
		{},
	}
	for _, c := range cases {
		s := FromUTF16(c)
		if got := ToUTF16(s); !slices.Equal(got, c) {
			t.Errorf("round trip %v: got %v via %q", c, got, s)
		}
		le := make([]byte, 0, 2*len(c))
		for _, u := range c {
			le = append(le, byte(u), byte(u>>8))
		}
		if s2 := string(AppendUTF16LE(nil, le)); s2 != s {
			t.Errorf("LE decode %v: got %q want %q", c, s2, s)
		}
	}
}

func TestValidUTF8IsUnchanged(t *testing.T) {
	s := "Fichier d'été 🌞.log"
	if got := FromUTF16(utf16.Encode([]rune(s))); got != s {
		t.Fatalf("got %q want %q", got, s)
	}
}

func FuzzToUTF16(f *testing.F) {
	f.Add("abc")
	f.Add("\xed\xa0\x80")
	f.Fuzz(func(t *testing.T, s string) {
		u := ToUTF16(s)
		// Re-encoding then decoding must be stable.
		s2 := FromUTF16(u)
		if !slices.Equal(ToUTF16(s2), u) {
			t.Fatalf("unstable for %q", s)
		}
	})
}
