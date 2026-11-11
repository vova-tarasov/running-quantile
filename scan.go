package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// maxDepth is the deepest container nesting a line may have. The top-level object is depth 1, as in encoding/json.
const maxDepth = 10000

// reason says why a line yields no value, or that it yields one.
type reason uint8

const (
	valid reason = iota
	malformedJSON
	notObject
	missingField
	notNumber
	outOfRange
	lineTooLong
)

// describe returns the warning text for r about field. A valid line has no warning, so its text is empty.
func (r reason) describe(field string) string {
	switch r {
	case malformedJSON:
		return "malformed JSON"
	case notObject:
		return "not a JSON object"
	case missingField:
		return fmt.Sprintf("missing field %q", field)
	case notNumber:
		return fmt.Sprintf("field %q is not a number", field)
	case outOfRange:
		return fmt.Sprintf("field %q is out of float64 range", field)
	case lineTooLong:
		return fmt.Sprintf("line too long (over %d MiB)", maxLineBytes>>20)
	}
	return ""
}

// extractNumber returns the number held by the top-level member named field of the JSON object on line,
// or the reason there is none. The whole line is validated in one pass, and the last matching member wins.
func extractNumber(line []byte, field string) (float64, reason) {
	s := scanner{line: line, field: field}
	i := skipSpace(line, 0)
	end := s.value(i, 0)
	if end < 0 || skipSpace(line, end) != len(line) {
		return 0, malformedJSON
	}
	if line[i] != '{' {
		return 0, notObject
	}
	if s.match == nil {
		return 0, missingField
	}
	if c := s.match[0]; c != '-' && (c < '0' || c > '9') {
		return 0, notNumber
	}
	if v, ok := smallInt(s.match); ok {
		return v, valid
	}
	v, err := strconv.ParseFloat(string(s.match), 64)
	if err != nil { // the grammar is already checked, so this is ErrRange
		return 0, outOfRange
	}
	if v == 0 {
		v = 0 // turns -0 into 0
	}
	return v, valid
}

// maxSmallIntDigits is the most digits smallInt converts. 10^15 < 2^53, so every such integer is a float64 exactly.
const maxSmallIntDigits = 15

// smallInt returns the value of num, a valid JSON number, if it is an integer of at most maxSmallIntDigits digits
// (no fraction, no exponent). The value is exact, so it is the one strconv.ParseFloat returns, except that -0 is 0.
// ok is false for any other number.
func smallInt(num []byte) (v float64, ok bool) {
	neg := num[0] == '-'
	if neg {
		num = num[1:]
	}
	if len(num) > maxSmallIntDigits {
		return 0, false
	}
	var n int64
	for _, c := range num {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n // an integer, so -0 is 0
	}
	return float64(n), true
}

// isJSONNumber reports whether all of s is one JSON number.
func isJSONNumber(s string) bool {
	return scanNumber([]byte(s), 0) == len(s)
}

// keyEquals reports whether a member key, given as its raw bytes between the quotes, decodes to field exactly as
// encoding/json decodes it: escapes are resolved, a surrogate pair becomes one rune, and a lone surrogate or a byte
// that is not valid UTF-8 becomes U+FFFD. raw must be the inside of a valid JSON string.
// The key is decoded a rune at a time and compared with field as it goes, so nothing is allocated.
func keyEquals(raw []byte, field string) bool {
	i := 0
	for ; i < len(raw) && raw[i] != '\\' && raw[i] < utf8.RuneSelf; i++ {
		if i == len(field) || raw[i] != field[i] {
			return false // plain ASCII decodes to itself, so the key and field differ at byte i
		}
	}
	j := i // field[:j] equals the key decoded so far
	for i < len(raw) {
		r, size := rune(raw[i]), 1
		switch {
		case r == '\\' && raw[i+1] == 'u':
			r, size = unicodeEscape(raw[i:]), 6
			if utf16.IsSurrogate(r) {
				// A high surrogate and a following low surrogate escape make one rune. Any other surrogate is
				// U+FFFD, and the escape after it is decoded on its own.
				if r = utf16.DecodeRune(r, unicodeEscape(raw[i+6:])); r != utf8.RuneError {
					size = 12
				}
			}
		case r == '\\':
			r, size = rune(raw[i+1]), 2 // \" \\ \/ stand for themselves
			if k := strings.IndexByte("bfnrt", raw[i+1]); k >= 0 {
				r = rune("\b\f\n\r\t"[k])
			}
		case r >= utf8.RuneSelf:
			r, size = utf8.DecodeRune(raw[i:]) // U+FFFD for a byte that is not valid UTF-8
		}
		i += size
		var enc [utf8.UTFMax]byte
		n := utf8.EncodeRune(enc[:], r)
		if len(field)-j < n || field[j:j+n] != string(enc[:n]) {
			return false
		}
		j += n
	}
	return j == len(field)
}

// unicodeEscape returns the UTF-16 code unit of the \uXXXX escape at the start of b, or -1 if b does not start
// with one. b comes from a valid JSON string, so a \u is always followed by four hex digits.
func unicodeEscape(b []byte) rune {
	if len(b) < 6 || b[0] != '\\' || b[1] != 'u' {
		return -1
	}
	var r rune
	for _, c := range b[2:6] {
		d := c - '0'
		if c > '9' {
			d = (c | 0x20) - 'a' + 10 // a-f in either case
		}
		r = r<<4 | rune(d)
	}
	return r
}

// scanner validates one line and remembers the value of the last top-level member whose key matches field.
type scanner struct {
	line  []byte
	field string
	match []byte // raw bytes of the last matching member's value; nil if none matched
}

// value returns the index just past the JSON value at line[i], or -1 if there is none or it is malformed.
// depth is the number of containers around the value.
func (s *scanner) value(i, depth int) int {
	if i >= len(s.line) {
		return -1
	}
	switch s.line[i] {
	case '{', '[':
		return s.container(i, depth+1)
	case '"':
		return skipString(s.line, i)
	case 't':
		return skipLiteral(s.line, i, "true")
	case 'f':
		return skipLiteral(s.line, i, "false")
	case 'n':
		return skipLiteral(s.line, i, "null")
	}
	return scanNumber(s.line, i)
}

// container returns the index just past the object or array at line[i], or -1 if it is malformed or nested
// deeper than maxDepth. depth is its own depth; members of the top-level object (depth 1) are matched against field.
func (s *scanner) container(i, depth int) int {
	if depth > maxDepth {
		return -1
	}
	b := s.line
	object := b[i] == '{'
	end := byte(']')
	if object {
		end = '}'
	}
	if i = skipSpace(b, i+1); i < len(b) && b[i] == end {
		return i + 1
	}
	for {
		var key []byte
		if object {
			k := i
			if i = skipString(b, i); i < 0 {
				return -1
			}
			key = b[k+1 : i-1]
			if i = skipSpace(b, i); i >= len(b) || b[i] != ':' {
				return -1
			}
			i = skipSpace(b, i+1)
		}
		v := i
		if i = s.value(i, depth); i < 0 {
			return -1
		}
		if object && depth == 1 && keyEquals(key, s.field) {
			s.match = b[v:i]
		}
		if i = skipSpace(b, i); i >= len(b) {
			return -1
		}
		if b[i] == end {
			return i + 1
		}
		if b[i] != ',' {
			return -1
		}
		i = skipSpace(b, i+1)
	}
}

// skipSpace returns the index of the first byte at or after b[i] that is not JSON whitespace.
func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// skipString returns the index just past the JSON string at b[i], or -1 if there is none or it is malformed.
// Bytes below 0x20 are rejected; any other byte, including invalid UTF-8, is accepted, as in encoding/json.
func skipString(b []byte, i int) int {
	if i >= len(b) || b[i] != '"' {
		return -1
	}
	for i++; i < len(b); i++ {
		switch c := b[i]; {
		case c == '"':
			return i + 1
		case c < 0x20:
			return -1
		case c == '\\':
			i++
			switch {
			case i < len(b) && strings.IndexByte(`"\/bfnrt`, b[i]) >= 0:
			case i+4 < len(b) && b[i] == 'u' && isHex(b[i+1]) && isHex(b[i+2]) && isHex(b[i+3]) && isHex(b[i+4]):
				i += 4
			default:
				return -1
			}
		}
	}
	return -1
}

// isHex reports whether c is a hexadecimal digit.
func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c|0x20 && c|0x20 <= 'f'
}

// skipLiteral returns the index just past lit at b[i], or -1 if b does not continue with lit.
func skipLiteral(b []byte, i int, lit string) int {
	if end := i + len(lit); end <= len(b) && string(b[i:end]) == lit {
		return end
	}
	return -1
}

// scanNumber returns the index just past the JSON number at b[i], or -1 if there is none or it is malformed.
// This is the only implementation of the RFC 8259 grammar -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
func scanNumber(b []byte, i int) int {
	if i < len(b) && b[i] == '-' {
		i++
	}
	if i < len(b) && b[i] == '0' {
		i++
	} else if i = skipDigits(b, i); i < 0 {
		return -1
	}
	if i < len(b) && b[i] == '.' {
		if i = skipDigits(b, i+1); i < 0 {
			return -1
		}
	}
	if i < len(b) && (b[i] == 'e' || b[i] == 'E') {
		i++
		if i < len(b) && (b[i] == '+' || b[i] == '-') {
			i++
		}
		if i = skipDigits(b, i); i < 0 {
			return -1
		}
	}
	return i
}

// skipDigits returns the index just past the run of decimal digits at b[i], or -1 if there is no digit there.
func skipDigits(b []byte, i int) int {
	start := i
	for i < len(b) && '0' <= b[i] && b[i] <= '9' {
		i++
	}
	if i == start {
		return -1
	}
	return i
}
