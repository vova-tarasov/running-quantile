package main

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"slices"
	"strconv"
	"testing"
)

// maxFuzzValues bounds the values FuzzTrackers feeds and FuzzProcess checks per input, so that every exec stays fast.
const maxFuzzValues = 2000

// FuzzTrackers checks that the adaptive, default and heap trackers match the exact quantile bit for bit after every
// Add. The input bytes become int16 values, whose small range forces many duplicates.
func FuzzTrackers(f *testing.F) {
	f.Add([]byte{0, 1, 0, 2, 0, 3, 0, 4}, 0.5)
	f.Add([]byte{0, 5, 0, 5, 0, 5, 0, 5, 0, 5, 0, 5, 0, 5, 0, 5, 0, 5, 0, 5}, 0.99)
	f.Add([]byte{255, 255, 0, 0, 127, 255, 128, 0}, 0.0)
	f.Add([]byte{255, 255, 0, 0, 127, 255, 128, 0}, 1.0)
	f.Add(bytes.Repeat([]byte{0, 1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 7, 0, 8, 0, 9, 0, 10}, 3), 0.25)
	f.Fuzz(func(t *testing.T, data []byte, q float64) {
		if math.IsNaN(q) || q < 0 || q > 1 {
			t.Skip()
		}
		trackers := map[string]tracker{
			"adaptive": newAdaptiveTracker(q, 8),
			"default":  newTracker(q),
			"heap":     newHeapTracker(q),
		}
		var sorted []float64 // the values so far, kept sorted so each check is O(n) instead of a full sort
		for i := 0; i+1 < len(data) && len(sorted) < maxFuzzValues; i += 2 {
			v := float64(int16(uint16(data[i])<<8 | uint16(data[i+1])))
			j, _ := slices.BinarySearch(sorted, v)
			sorted = slices.Insert(sorted, j, v)
			want := sortedQuantile(sorted, q)
			for name, tr := range trackers {
				tr.Add(v)
				if got := tr.Quantile(); math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("%s tracker after %d values, q=%v: got %v, want %v", name, len(sorted), q, got, want)
				}
			}
		}
	})
}

// FuzzProcess checks that process never panics or fails on arbitrary input, writes one parsable number per valid
// record, and nothing else. For the first maxFuzzValues valid records it also checks that each line is the median of
// the valid values so far as appendFloat formats it, which covers the scanner -> tracker -> formatter seam.
func FuzzProcess(f *testing.F) {
	f.Add([]byte("{\"v\":1}\n{\"v\":2}\n{\"v\":3}\n"), "v")
	f.Add([]byte("\xef\xbb\xbf{\"v\":1}\r\n\r\n  \t\n{\"v\":-0.0}"), "v")
	f.Add([]byte("{\"v\":1\n[1]\n{\"w\":1}\n{\"v\":\"x\"}\n{\"v\":1e999}\n{\"v\":2}"), "v")
	f.Add([]byte("{\"caf\\u00e9\":7}\n{\"café\":8}\n"), "café")
	f.Fuzz(func(t *testing.T, content []byte, field string) {
		const q = 0.5
		var out bytes.Buffer
		w := bufio.NewWriterSize(&out, 1<<16)
		if _, err := process(bytes.NewReader(content), q, field, w, io.Discard); err != nil {
			t.Fatalf("process returned %v", err)
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		want := 0
		var sorted []float64   // the first maxFuzzValues valid values, sorted
		var wantLines []string // the expected output line after each of them
		for n, line := range bytes.Split(content, []byte("\n")) {
			line = bytes.TrimSuffix(line, []byte("\r"))
			if n == 0 {
				line = bytes.TrimPrefix(line, utf8BOM)
			}
			if len(bytes.TrimLeft(line, " \t\r")) == 0 {
				continue
			}
			v, r := extractNumber(line, field)
			if r != valid {
				continue
			}
			want++
			if len(sorted) < maxFuzzValues {
				j, _ := slices.BinarySearch(sorted, v)
				sorted = slices.Insert(sorted, j, v)
				wantLines = append(wantLines, string(appendFloat(nil, sortedQuantile(sorted, q))))
			}
		}
		got := 0
		for _, l := range bytes.SplitAfter(out.Bytes(), []byte("\n")) {
			if len(l) == 0 {
				continue // SplitAfter leaves an empty piece after the final newline
			}
			if l[len(l)-1] != '\n' {
				t.Fatalf("output line %q lacks a newline", l)
			}
			l = l[:len(l)-1]
			if got < len(wantLines) && string(l) != wantLines[got] {
				t.Fatalf("output line %d is %q, want %q", got+1, l, wantLines[got])
			}
			if _, err := strconv.ParseFloat(string(l), 64); err != nil {
				t.Fatalf("output line %q does not parse: %v", l, err)
			}
			got++
		}
		if got != want {
			t.Fatalf("got %d output lines, want %d", got, want)
		}
	})
}
