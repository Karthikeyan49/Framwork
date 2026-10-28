package ai

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// foldLookalikes writes s the way a person reads it when looking for a secret: a character drawn like an ASCII letter, digit or
// sign (a fullwidth letter, a Cyrillic "s", a circled or mathematical letter) becomes that character, a space or a dash of another
// block becomes " " or "-", and a character that is invisible inside a word (soft hyphen, joiners, variation selectors) is dropped.
// It returns s itself when nothing changes, and it is for matching only: redactText keeps its result only when it shows a secret.
//
// The table is short on purpose. It is not the Unicode confusables list: accent marks ("ś" is one character, not "s" and an
// accent), the other scripts and every mapping not listed below stay as they are.
func foldLookalikes(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	changed := false
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		f, keep := foldRune(r)
		if !keep || f != r {
			changed = true
		}
		if keep {
			b.WriteRune(f)
		}
	}
	if !changed {
		return s
	}
	return b.String()
}

// foldRune returns the character r reads as and whether it is kept at all.
func foldRune(r rune) (rune, bool) {
	switch {
	case r < utf8.RuneSelf:
		return r, true
	case r >= 0xff01 && r <= 0xff5e: // fullwidth forms of the ASCII punctuation, digits and letters
		return r - 0xfee0, true
	case r == 0xa0 || r >= 0x2000 && r <= 0x200a || r == 0x202f || r == 0x205f || r == 0x3000: // no-break, en, em, thin, ideographic spaces
		return ' ', true
	case r >= 0x2010 && r <= 0x2015 || r == 0x2212 || r == 0xfe63: // hyphens, dashes, minus sign
		return '-', true
	case unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) || unicode.Is(unicode.Variation_Selector, r):
		return 0, false
	case r >= 0x1d400 && r <= 0x1d6a3: // mathematical bold, italic, script, fraktur, double-struck, sans-serif and monospace letters
		if c := (r - 0x1d400) % 52; c >= 26 {
			return 'a' + c - 26, true
		} else {
			return 'A' + c, true
		}
	case r >= 0x1d7ce && r <= 0x1d7ff: // mathematical digits
		return '0' + (r-0x1d7ce)%10, true
	case r >= 0x24b6 && r <= 0x24cf: // circled capitals
		return 'A' + (r - 0x24b6), true
	case r >= 0x24d0 && r <= 0x24e9: // circled small letters
		return 'a' + (r - 0x24d0), true
	}
	if a, ok := lookalikes[r]; ok {
		return a, true
	}
	return r, true
}

// lookalikes maps Cyrillic, Greek and Latin-extension letters that are drawn like an ASCII letter to it: "ѕk-" written with a
// Cyrillic "ѕ" is a key prefix to a reader and nothing to a pattern. Pairs of code point and letter.
var lookalikes = func() map[rune]rune {
	pairs := []rune{
		// Cyrillic capitals
		0x0405, 'S', 0x0406, 'I', 0x0408, 'J', 0x0410, 'A', 0x0412, 'B', 0x0415, 'E', 0x041a, 'K', 0x041c, 'M', 0x041d, 'H',
		0x041e, 'O', 0x0420, 'P', 0x0421, 'C', 0x0422, 'T', 0x0425, 'X', 0x04ae, 'Y', 0x04c0, 'I',
		// Cyrillic small letters
		0x0430, 'a', 0x0435, 'e', 0x043e, 'o', 0x0440, 'p', 0x0441, 'c', 0x0443, 'y', 0x0445, 'x', 0x0455, 's', 0x0456, 'i',
		0x0458, 'j', 0x04bb, 'h', 0x04cf, 'l', 0x0501, 'd', 0x051b, 'q', 0x051d, 'w',
		// Greek capitals
		0x0391, 'A', 0x0392, 'B', 0x0395, 'E', 0x0396, 'Z', 0x0397, 'H', 0x0399, 'I', 0x039a, 'K', 0x039c, 'M', 0x039d, 'N',
		0x039f, 'O', 0x03a1, 'P', 0x03a4, 'T', 0x03a5, 'Y', 0x03a7, 'X',
		// Greek small letters
		0x03b1, 'a', 0x03b9, 'i', 0x03ba, 'k', 0x03bd, 'v', 0x03bf, 'o', 0x03c1, 'p', 0x03c5, 'u', 0x03c7, 'x',
		// Latin extensions: dotless i, alpha, script g, script l
		0x0131, 'i', 0x0251, 'a', 0x0261, 'g', 0x2113, 'l',
	}
	m := make(map[rune]rune, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return m
}()
