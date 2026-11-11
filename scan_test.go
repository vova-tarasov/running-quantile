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

// TestExtractNumberValid checks the value extracted from valid records, including unusual number spellings.
func TestExtractNumberValid(t *testing.T) {
	digits300 := strings.Repeat("123456789", 33) + "123"
	want300, err := strconv.ParseFloat(digits300, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
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
	} {
		got, r := extractNumber([]byte(c.line), c.field)
		if r != valid || math.Float64bits(got) != math.Float64bits(c.want) {
			t.Errorf("extractNumber(%q, %q) = (%v, %v), want (%v, valid)", c.line, c.field, got, r, c.want)
		}
	}
}

// TestExtractNumberReasons checks the reason given for every kind of invalid record in spec §6.3.
func TestExtractNumberReasons(t *testing.T) {
	for _, c := range []struct {
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
	} {
		if got, r := extractNumber([]byte(c.line), c.field); r != c.want || got != 0 {
			t.Errorf("extractNumber(%q, %q) = (%v, %v), want (0, %v)", c.line, c.field, got, r, c.want)
		}
	}
}

// TestExtractNumberDepthLimit checks that nesting is capped exactly where encoding/json caps it.
func TestExtractNumberDepthLimit(t *testing.T) {
	for _, c := range []struct {
		depth int
		want  reason
	}{{10000, missingField}, {10001, malformedJSON}} {
		n := c.depth - 1 // arrays nested inside the top-level object
		line := []byte(`{"a":` + strings.Repeat("[", n) + strings.Repeat("]", n) + `}`)
		if _, r := extractNumber(line, "v"); r != c.want {
			t.Errorf("depth %d: reason %v, want %v", c.depth, r, c.want)
		}
		if json.Valid(line) != (c.want != malformedJSON) {
			t.Errorf("depth %d: json.Valid = %v disagrees with %v", c.depth, json.Valid(line), c.want)
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

// TestExtractNumberDoesNotAllocate checks that scanning a typical valid record allocates nothing.
func TestExtractNumberDoesNotAllocate(t *testing.T) {
	line := []byte(pdfLine)
	if n := testing.AllocsPerRun(1000, func() { extractNumber(line, "latency_ms") }); n != 0 {
		t.Errorf("extractNumber allocated %v times per run, want 0", n)
	}
}
