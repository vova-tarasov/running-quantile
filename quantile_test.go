package main

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// oracleQs are the quantiles every oracle test checks: edge cases plus one seeded random q in (0,1).
var oracleQs = append([]float64{0, 5e-324, 0.01, 0.25, 0.5, 0.75, 0.99, 0.9999999999999999, 1},
	rand.New(rand.NewPCG(7, 8)).Float64())

// referenceQuantile returns the type 7 quantile of seen by sorting a copy.
func referenceQuantile(seen []float64, q float64) float64 {
	s := slices.Clone(seen)
	slices.Sort(s)
	k, f := rank(len(s), q)
	if f == 0 {
		return s[k]
	}
	return lerp(s[k], s[k+1], f)
}

// oracleInputs returns fresh, deterministically seeded generators of test inputs by name; call i sequentially from 0.
func oracleInputs() map[string]func(i int) float64 {
	rnd := func(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed+1)) }
	r1, r2, r3, r4 := rnd(1), rnd(2), rnd(3), rnd(4)
	return map[string]func(i int) float64{
		"random":     func(int) float64 { return r1.NormFloat64() * 100 },
		"smallInts":  func(int) float64 { return float64(r2.IntN(10)) },
		"ascending":  func(i int) float64 { return float64(i) },
		"descending": func(i int) float64 { return float64(-i) },
		"constant":   func(int) float64 { return 42 },
		"mixedSign":  func(int) float64 { return (r3.Float64()*2 - 1) * 1000 },
		"extremes": func(i int) float64 {
			switch {
			case i%5 == 4:
				return r4.NormFloat64()
			case i%2 == 0:
				return math.MaxFloat64
			}
			return -math.MaxFloat64
		},
	}
}

// TestRank checks rank on the exact cases of the definition, and that h is rounded to float64 before k is
// subtracted: for n = 101 and q = 0.29, h is 28.999999999999996, while a fused multiply-subtract gives f
// 0.999999999999998.
func TestRank(t *testing.T) {
	for _, c := range []struct {
		n int
		q float64
		k int
		f float64
	}{{1, 0.75, 0, 0}, {2, 0.75, 0, 0.75}, {6, 0.75, 3, 0.75}, {5, 1, 4, 0}, {101, 0.29, 28, 0.9999999999999964}} {
		if k, f := rank(c.n, c.q); k != c.k || f != c.f {
			t.Errorf("rank(%d, %v) = (%d, %v), want (%d, %v)", c.n, c.q, k, f, c.k, c.f)
		}
	}
}

// TestLerp checks interpolation, including the overflow fallback, and that every product is rounded to float64
// before it is added: a fused multiply-add gives 1.7 and -1.190772539449263e+307 for the last two rows.
func TestLerp(t *testing.T) {
	for _, c := range []struct{ a, b, f, want float64 }{
		{3, 5, 0.75, 4.5},
		{-math.MaxFloat64, math.MaxFloat64, 0.5, 0},
		{2, 2, 0.3, 2},
		{1, 8, 0.1, 1.7000000000000002},
		{-math.MaxFloat64, 1e308, 0.6, -1.1907725394492634e+307},
	} {
		if got := lerp(c.a, c.b, c.f); got != c.want {
			t.Errorf("lerp(%v, %v, %v) = %v, want %v", c.a, c.b, c.f, got, c.want)
		}
	}
}

// TestHeapTrackerSpecExample checks the worked example of the spec.
func TestHeapTrackerSpecExample(t *testing.T) {
	tr := newHeapTracker(0.75)
	want := []float64{5, 4.5, 6.5, 5.75, 8, 7.25}
	for i, v := range []float64{5, 3, 8, 1, 9, 4} {
		tr.Add(v)
		if got := tr.Quantile(); got != want[i] {
			t.Errorf("after Add #%d: got %v, want %v", i+1, got, want[i])
		}
	}
}

// TestTrackersMatchReference compares every tracker bit for bit with referenceQuantile on every oracle input and q.
func TestTrackersMatchReference(t *testing.T) {
	for name := range oracleInputs() {
		for _, q := range oracleQs {
			gen := oracleInputs()[name]
			tr := newTracker(q)
			var seen []float64
			for i := range 600 {
				v := gen(i)
				seen = append(seen, v)
				tr.Add(v)
				got, want := tr.Quantile(), referenceQuantile(seen, q)
				if math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("input %s, q %v, step %d: got %v, want %v", name, q, i+1, got, want)
				}
			}
		}
	}
}

// TestMinMaxTrackersUseConstantMemory checks that q = 0 and q = 1 allocate almost nothing.
func TestMinMaxTrackersUseConstantMemory(t *testing.T) {
	for _, q := range []float64{0, 1} {
		tr := newTracker(q)
		got := allocatedBytes(func() {
			for i := range 1_000_000 {
				tr.Add(float64(i))
			}
		})
		if got > 1024 {
			t.Errorf("q %v: allocated %d bytes, limit 1024", q, got)
		}
	}
}
