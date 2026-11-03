package tools

// HasLoneSurrogateEscape reports whether the JSON text holds, inside a string, the escape of one half of a surrogate pair that the other half does
// not complete: \ud800 to \udbff not directly followed by \udc00 to \udfff, or \udc00 to \udfff not directly preceded by a high half. The JSON
// decoder of the standard library replaces such an escape by U+FFFD without a word, so two different texts become one value, which is what the check
// for invalid UTF-8 bytes (independent red team TS-04) removes for raw bytes and not for the escape (independent review TF-07). The escape of U+FFFD
// itself (the escape with the digits fffd), a pair and every other escape are fine. A text that is not well-formed JSON is not judged here: the decoder refuses it.
func HasLoneSurrogateEscape(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			inString = c == '"'
			continue
		}
		switch c {
		case '"':
			inString = false
		case '\\':
			if i++; i >= len(raw) || raw[i] != 'u' {
				continue // another escape: the character after the backslash is read as part of it
			}
			unit, ok := hexUnit(raw, i+1)
			if !ok {
				return false
			}
			i += 4 // the last digit of the escape
			switch {
			case unit >= 0xdc00 && unit < 0xe000:
				return true
			case unit >= 0xd800 && unit < 0xdc00:
				if i+6 < len(raw) && raw[i+1] == '\\' && raw[i+2] == 'u' {
					if low, ok := hexUnit(raw, i+3); ok && low >= 0xdc00 && low < 0xe000 {
						i += 6 // the second half is read with its first
						continue
					}
				}
				return true
			}
		}
	}
	return false
}

// hexUnit reads the four hexadecimal digits at raw[at:].
func hexUnit(raw []byte, at int) (rune, bool) {
	if at+4 > len(raw) {
		return 0, false
	}
	var unit rune
	for _, c := range raw[at : at+4] {
		switch {
		case c >= '0' && c <= '9':
			unit = unit<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			unit = unit<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			unit = unit<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return unit, true
}
