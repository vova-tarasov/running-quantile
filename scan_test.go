package main

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

// pdfLine is the first record of the example in the task PDF.
const pdfLine = `{"timestamp": "2026-06-18T10:00:01Z", "customer_id": "acme", "region": "us-east", "endpoint": "/api/v1/query", "latency_ms": 5, "bytes_sent": 8421}`

// digits300 is a 300-digit integer, which is still finite as a float64.
var digits300 = strings.Repeat("123456789", 33) + "123"

// want300 is digits300 as strconv.ParseFloat parses it.
var want300, _ = strconv.ParseFloat(digits300, 64)

// validRecords lists valid records and the value extractNumber must return for each.
var validRecords = []struct {
	line, field string
	want        float64
}{
	{`{"v": 5}`, "v", 5},
	{pdfLine, "latency_ms", 5},
	{`  {"v":1}  `, "v", 1},
	{"\t{\n\"v\"\r:\t5\r\n}\n", "v", 5},
	{`{"a":[1,{"b":null}],"v":-2.5,"c":"x"}`, "v", -2.5},
	{`{"v":"x","v":2}`, "v", 2},
	{"{\"s\":\"\xff\",\"v\":5}", "v", 5},
	{`{"s":"\"\\\/\b\f\n\r\t\u00e9\uD83D\uDE00","v":5}`, "v", 5},
	{`{"t":true,"f":false,"n":null,"v":7}`, "v", 7},
	{`{"v":1E+2}`, "v", 100},
	{`{"v":-0.0}`, "v", 0},
	{`{"v":0e0}`, "v", 0},
	{`{"v":1e-400}`, "v", 0},
	{`{"v":9007199254740993}`, "v", 9007199254740992},
	{`{"v":` + digits300 + `}`, "v", want300},
	{`{"v":5e-324}`, "v", math.SmallestNonzeroFloat64},
}

// TestExtractNumberValid checks the value extracted from valid records, including unusual number spellings.
func TestExtractNumberValid(t *testing.T) {
	for _, c := range validRecords {
		got, r := extractNumber([]byte(c.line), c.field)
		if r != valid || math.Float64bits(got) != math.Float64bits(c.want) {
			t.Errorf("extractNumber(%q, %q) = (%v, %v), want (%v, valid)", c.line, c.field, got, r, c.want)
		}
	}
}

// invalidRecords lists invalid records and the reason extractNumber must give for each.
var invalidRecords = []struct {
	line, field string
	want        reason
}{
	{`{"v": 5`, "v", malformedJSON},
	{`{"v": 5} x`, "v", malformedJSON},
	{`{"v": NaN}`, "v", malformedJSON},
	{`{"v": Infinity}`, "v", malformedJSON},
	{`{"v": 05}`, "v", malformedJSON},
	{`{"v": +5}`, "v", malformedJSON},
	{`{"v": .5}`, "v", malformedJSON},
	{`{"v": 1.}`, "v", malformedJSON},
	{`{"v": 1e}`, "v", malformedJSON},
	{`{"v": -}`, "v", malformedJSON},
	{"{\"v\": \"a\tb\"}", "v", malformedJSON},
	{`{"v": "\x"}`, "v", malformedJSON},
	{`{"v": "\u12"}`, "v", malformedJSON},
	{`{"v": "abc`, "v", malformedJSON},
	{`{"v": "abc\`, "v", malformedJSON},
	{`{"v" 5}`, "v", malformedJSON},
	{`{"v": 5,}`, "v", malformedJSON},
	{`{,}`, "v", malformedJSON},
	{`{"v": tru}`, "v", malformedJSON},
	{`{"v": [1}`, "v", malformedJSON},
	{"{\"v\":\f5}", "v", malformedJSON},
	{"\x00", "v", malformedJSON},
	{"", "v", malformedJSON},
	{`[1]`, "v", notObject},
	{`5`, "v", notObject},
	{`"x"`, "v", notObject},
	{`null`, "v", notObject},
	{`{}`, "v", missingField},
	{`{"V": 5}`, "v", missingField},
	{`{"x":{"v":1}}`, "v", missingField},
	{`{"note":"\"v\": 5"}`, "v", missingField},
	{`{"vv":1}`, "v", missingField},
	{`{"v_ms":1}`, "v", missingField},
	{`{"\u0076":1}`, `\u0076`, missingField},
	{`{"v":"5"}`, "v", notNumber},
	{`{"v":true}`, "v", notNumber},
	{`{"v":null}`, "v", notNumber},
	{`{"v":[]}`, "v", notNumber},
	{`{"v":{}}`, "v", notNumber},
	{`{"v":2,"v":"x"}`, "v", notNumber},
	{`{"v":1e400}`, "v", outOfRange},
	{`{"v":-1e400}`, "v", outOfRange},
	{`{"v":` + strings.Repeat("1234567890", 40) + `}`, "v", outOfRange},
}

// TestExtractNumberReasons checks the reason given for every kind of invalid record in spec §6.3.
func TestExtractNumberReasons(t *testing.T) {
	for _, c := range invalidRecords {
		if got, r := extractNumber([]byte(c.line), c.field); r != c.want || got != 0 {
			t.Errorf("extractNumber(%q, %q) = (%v, %v), want (0, %v)", c.line, c.field, got, r, c.want)
		}
	}
}

// TestExtractNumberDepthLimit checks that nesting is capped exactly where encoding/json caps it,
// under a top-level object and under a top-level array.
func TestExtractNumberDepthLimit(t *testing.T) {
	nest := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	for _, c := range []struct {
		name, line string
		want       reason
	}{
		{"object at depth 10000", `{"a":` + nest(9999) + `}`, missingField},
		{"object at depth 10001", `{"a":` + nest(10000) + `}`, malformedJSON},
		{"array at depth 10000", nest(10000), notObject},
		{"array at depth 10001", nest(10001), malformedJSON},
	} {
		line := []byte(c.line)
		if _, r := extractNumber(line, "v"); r != c.want {
			t.Errorf("%s: reason %v, want %v", c.name, r, c.want)
		}
		if json.Valid(line) != (c.want != malformedJSON) {
			t.Errorf("%s: json.Valid = %v disagrees with %v", c.name, json.Valid(line), c.want)
		}
	}
}

// TestReasonDescribe checks the exact warning text of every reason.
func TestReasonDescribe(t *testing.T) {
	for r, want := range map[reason]string{
		valid:         "",
		malformedJSON: "malformed JSON",
		notObject:     "not a JSON object",
		missingField:  `missing field "v"`,
		notNumber:     `field "v" is not a number`,
		outOfRange:    `field "v" is out of float64 range`,
		lineTooLong:   "line too long (over 64 MiB)",
	} {
		if got := r.describe("v"); got != want {
			t.Errorf("reason %d: describe = %q, want %q", r, got, want)
		}
	}
	if got, want := missingField.describe(`a"b`), `missing field "a\"b"`; got != want {
		t.Errorf("describe = %q, want %q", got, want)
	}
}

// TestIsJSONNumber checks isJSONNumber against the JSON number grammar.
func TestIsJSONNumber(t *testing.T) {
	for _, s := range []string{"0", "-0", "0.75", "1", "1e-1", "1E+2"} {
		if !isJSONNumber(s) {
			t.Errorf("isJSONNumber(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", ".5", "+0.5", "1.", "05", "0x1p-2", "1_0", "NaN", "Inf", " 0.5", "0.5 "} {
		if isJSONNumber(s) {
			t.Errorf("isJSONNumber(%q) = true, want false", s)
		}
	}
}

// TestExtractNumberDoesNotAllocate checks that scanning a typical valid record allocates nothing, also when the
// number is 32 bytes long: the most a string conversion can hold on the stack, so only a conversion that escapes
// would allocate there (a 1-byte number never allocates, escaping or not).
func TestExtractNumberDoesNotAllocate(t *testing.T) {
	for _, c := range []struct{ line, field string }{
		{pdfLine, "latency_ms"},
		{`{"v":-1.234567890123456789012345e-100}`, "v"},
	} {
		line := []byte(c.line)
		if _, r := extractNumber(line, c.field); r != valid {
			t.Fatalf("extractNumber(%q, %q) gives reason %v, want valid", c.line, c.field, r)
		}
		if n := testing.AllocsPerRun(1000, func() { extractNumber(line, c.field) }); n != 0 {
			t.Errorf("extractNumber(%q, %q) allocated %v times per run, want 0", c.line, c.field, n)
		}
	}
}

// keyCases lists raw keys, as written between the quotes, and whether each one names field.
var keyCases = []struct {
	raw, field string
	want       bool
}{
	{`v`, "v", true},
	{`a\"b`, `a"b`, true},
	{`a\\b`, `a\b`, true},
	{`\/\b\f\n\r\t`, "/\b\f\n\r\t", true},
	{`caf\u00e9`, "café", true},
	{"café", "café", true},
	{`\uD83D\uDE00`, "😀", true}, // hex digits in either case
	{`\ud83d\ude00`, "😀", true},
	{`\ud800`, "\ufffd", true},
	{`\ud800v`, "\ufffdv", true},
	{`\ud800\u0041`, "\ufffdA", true}, // a lone surrogate's following \u is decoded on its own
	{`\udc00`, "\ufffd", true},
	{"\xff", "\ufffd", true},
	{"\xff", "\xff", false}, // a field that is not valid UTF-8 never matches
	{`v`, "vv", false},
	{`v`, "V", false},
	{`caf\u00e9`, "cafe", false},
	{`caf\u00e9`, "caf", false},
	{`caf\u00e9`, "café!", false},
}

// TestKeyEquals checks that raw keys are decoded the way encoding/json decodes them before being compared.
func TestKeyEquals(t *testing.T) {
	for _, c := range keyCases {
		if got := keyEquals([]byte(c.raw), c.field); got != c.want {
			t.Errorf("keyEquals(%q, %q) = %v, want %v", c.raw, c.field, got, c.want)
		}
	}
}

// unicodeKeyRecords lists records holding 5 under field, whose key is written raw (escaped only where JSON
// requires it) or fully escaped, followed by a member that must not match.
var unicodeKeyRecords = []struct{ line, field string }{
	{`{"lat\"ency":5,"x":"y"}`, `lat"ency`},
	{`{"lat\u0022ency":5,"x":"y"}`, `lat"ency`},
	{`{"a\\b":5,"x":"y"}`, `a\b`},
	{`{"a\u005cb":5,"x":"y"}`, `a\b`},
	{`{"café":5,"x":"y"}`, "café"},
	{`{"caf\u00e9":5,"x":"y"}`, "café"},
	{`{"😀":5,"x":"y"}`, "😀"},
	{`{"\ud83d\ude00":5,"x":"y"}`, "😀"},
}

// TestExtractNumberMatchesEscapedAndUnicodeKeys checks that fields needing escapes or outside ASCII match their
// key in both the raw and the escaped spelling.
func TestExtractNumberMatchesEscapedAndUnicodeKeys(t *testing.T) {
	for _, c := range unicodeKeyRecords {
		if got, r := extractNumber([]byte(c.line), c.field); r != valid || got != 5 {
			t.Errorf("extractNumber(%q, %q) = (%v, %v), want (5, valid)", c.line, c.field, got, r)
		}
	}
}

// TestExtractNumberWithEscapedKeyDoesNotAllocate checks that a key that must be decoded is compared without
// allocating.
func TestExtractNumberWithEscapedKeyDoesNotAllocate(t *testing.T) {
	for _, c := range []struct{ line, field string }{
		{`{"\u0076": 5}`, "v"},
		{`{"\ud83d\ude00": 5}`, "😀"},
	} {
		line := []byte(c.line)
		if got, r := extractNumber(line, c.field); r != valid || got != 5 {
			t.Fatalf("extractNumber(%q, %q) = (%v, %v), want (5, valid)", c.line, c.field, got, r)
		}
		if n := testing.AllocsPerRun(1000, func() { extractNumber(line, c.field) }); n != 0 {
			t.Errorf("extractNumber(%q, %q) allocated %v times per run, want 0", c.line, c.field, n)
		}
	}
}
