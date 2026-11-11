package main

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"testing"
)

// FuzzExtractNumber checks that extractNumber agrees with oracleExtract on any line and field:
// the same reason, and the same value bits.
func FuzzExtractNumber(f *testing.F) {
	for _, c := range validRecords {
		f.Add([]byte(c.line), c.field)
	}
	for _, c := range invalidRecords {
		f.Add([]byte(c.line), c.field)
	}
	for _, c := range keyCases {
		f.Add([]byte(`{"`+c.raw+`":5}`), c.field)
	}
	for _, c := range unicodeKeyRecords {
		f.Add([]byte(c.line), c.field)
	}
	f.Fuzz(func(t *testing.T, line []byte, field string) {
		got, r := extractNumber(line, field)
		want, wantR := oracleExtract(line, field)
		if r != wantR || math.Float64bits(got) != math.Float64bits(want) {
			t.Errorf("extractNumber(%q, %q) = (%v, %v), encoding/json gives (%v, %v)", line, field, got, r, want, wantR)
		}
	})
}

// oracleExtract is extractNumber rebuilt from encoding/json, the reference the scanner is fuzzed against.
func oracleExtract(line []byte, field string) (float64, reason) {
	if !json.Valid(line) {
		return 0, malformedJSON
	}
	// Not Unmarshal into any: it fails on valid objects holding a number beyond float64, such as 1e400.
	if bytes.TrimLeft(line, " \t\r\n")[0] != '{' {
		return 0, notObject
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(line, &members); err != nil {
		panic(err) // every valid object unmarshals into this map
	}
	raw, ok := members[field]
	if !ok {
		return 0, missingField
	}
	if c := raw[0]; c != '-' && (c < '0' || c > '9') {
		return 0, notNumber
	}
	v, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return 0, outOfRange
	}
	if v == 0 {
		v = 0 // turns -0 into 0
	}
	return v, valid
}
