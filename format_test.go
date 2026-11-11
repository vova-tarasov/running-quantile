package main

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"testing"
)

// TestAppendFloatExamples checks fixed inputs against their expected spellings.
func TestAppendFloatExamples(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{5, "5"}, {4.5, "4.5"}, {7.25, "7.25"},
		{1234567, "1234567"}, {-2.5, "-2.5"}, {0, "0"},
		{1e-6, "0.000001"}, {1e-7, "1e-7"},
		{1e20, "100000000000000000000"}, {1e21, "1e+21"},
		{math.MaxFloat64, "1.7976931348623157e+308"},
		{5e-324, "5e-324"},
		{9007199254740992, "9007199254740992"},
	}
	for _, c := range cases {
		if got := string(appendFloat(nil, c.in)); got != c.want {
			t.Errorf("appendFloat(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAppendFloatMatchesEncodingJSON compares appendFloat with json.Marshal on random finite values.
func TestAppendFloatMatchesEncodingJSON(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	check := func(v float64) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return
		}
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if got := appendFloat(nil, v); string(got) != string(want) {
			t.Fatalf("appendFloat(%v) = %q, json = %q", v, got, want)
		}
	}
	for i := 0; i < 100000; i++ {
		v := math.Float64frombits(r.Uint64())
		check(v)
		check(r.NormFloat64() * math.Pow(10, float64(r.IntN(661)-330)))
	}
}

// TestAppendFloatDoesNotAllocate checks that formatting into a pre-sized buffer allocates nothing.
func TestAppendFloatDoesNotAllocate(t *testing.T) {
	dst := make([]byte, 0, 64)
	for _, v := range []float64{4.5, 1e-7, 1e21, math.MaxFloat64} {
		if n := testing.AllocsPerRun(100, func() { dst = appendFloat(dst[:0], v) }); n != 0 {
			t.Errorf("appendFloat(%v) allocs = %v, want 0", v, n)
		}
	}
}
