package main

import (
	"math"
	"math/rand/v2"
	"testing"
)

// distinctInputs returns the oracle inputs plus two duplicate-heavy generators.
func distinctInputs() map[string]func(i int) float64 {
	m := oracleInputs()
	m["few values in long runs"] = func(i int) float64 { return float64((i / 50) % 3) }
	r := rand.New(rand.NewPCG(9, 10))
	m["duplicates then all new"] = func(i int) float64 {
		if i < 500 {
			return float64(r.IntN(10))
		}
		return float64(i) + 0.5
	}
	return m
}

// TestDistinctMatchesHeapsBitForBit checks the distinct tracker against the heap tracker after every Add.
func TestDistinctMatchesHeapsBitForBit(t *testing.T) {
	for name := range distinctInputs() {
		for _, q := range oracleQs {
			gd, gh := distinctInputs()[name], distinctInputs()[name]
			d, h := newDistinctTracker(q), newHeapTracker(q)
			for i := 0; i < 3000; i++ {
				d.Add(gd(i))
				h.Add(gh(i))
				if got, want := d.Quantile(), h.Quantile(); math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("%s q=%v after %d adds: got %v, want %v", name, q, i+1, got, want)
				}
			}
		}
	}
}

// TestDistinctToHeapsPreservesState checks that a converted heapTracker has the exact low/high split and quantile
// at once, and then continues like a fresh one fed the same values (restarting the generators after conversion).
func TestDistinctToHeapsPreservesState(t *testing.T) {
	type tc struct {
		name string
		q    float64
		seq  func(i int) float64
		at   []int
	}
	cases := []tc{{"straddle", 0.5, func(i int) float64 {
		if i < 600 {
			return 0
		}
		return 1
	}, []int{600, 1000, 1200}}}
	for name := range distinctInputs() {
		for _, q := range oracleQs {
			cases = append(cases, tc{name, q, nil, []int{1, 2, 7, 100, 999}})
		}
	}
	for _, c := range cases {
		for _, at := range c.at {
			seq1, seq2, seq3 := c.seq, c.seq, c.seq
			if c.seq == nil {
				seq1, seq2, seq3 = distinctInputs()[c.name], distinctInputs()[c.name], distinctInputs()[c.name]
			}
			d, fresh := newDistinctTracker(c.q), newHeapTracker(c.q)
			for i := 0; i < at; i++ {
				d.Add(seq1(i))
				fresh.Add(seq2(i))
			}
			h := d.toHeaps()
			if k, _ := rank(at, c.q); h.low.len() != k+1 {
				t.Fatalf("%s q=%v converted at %d: low has %d values, want %d", c.name, c.q, at, h.low.len(), k+1)
			}
			if got, want := h.Quantile(), fresh.Quantile(); math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("%s q=%v converted at %d: quantile %v, want %v", c.name, c.q, at, got, want)
			}
			for i := at; i < at+1500; i++ {
				v := seq3(i)
				if i >= at+1000 {
					v = float64(i%7) - 3
				}
				h.Add(v)
				fresh.Add(v)
				if got, want := h.Quantile(), fresh.Quantile(); math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("%s q=%v converted at %d, add %d: got %v, want %v", c.name, c.q, at, i, got, want)
				}
			}
		}
	}
}

// TestDistinctMemoryFollowsDistinctValues checks that a million records of 100 distinct values stay small.
func TestDistinctMemoryFollowsDistinctValues(t *testing.T) {
	d := newDistinctTracker(0.5)
	got := allocatedBytes(func() {
		for i := 0; i < 1_000_000; i++ {
			d.Add(float64(i % 100))
		}
	})
	if got > 256<<10 {
		t.Errorf("allocated %d bytes, want at most 256 KiB", got)
	}
	if d.distinct() != 100 || d.n != 1_000_000 {
		t.Errorf("distinct=%d n=%d", d.distinct(), d.n)
	}
}

// TestAdaptiveMatchesHeapsBitForBit checks the adaptive tracker against the heap tracker after every Add,
// including the Add that triggers the switch.
func TestAdaptiveMatchesHeapsBitForBit(t *testing.T) {
	for name := range distinctInputs() {
		for _, q := range oracleQs {
			ga, gh := distinctInputs()[name], distinctInputs()[name]
			a, h := newAdaptiveTracker(q, 64), newHeapTracker(q)
			for i := 0; i < 3000; i++ {
				a.Add(ga(i))
				h.Add(gh(i))
				if got, want := a.Quantile(), h.Quantile(); math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("%s q=%v after %d adds: got %v, want %v", name, q, i+1, got, want)
				}
			}
		}
	}
}

// TestAdaptiveSwitchesOnHighCardinality checks that 1000 distinct values switch to heaps once past the floor.
func TestAdaptiveSwitchesOnHighCardinality(t *testing.T) {
	a := newAdaptiveTracker(0.5, 64)
	for i := 0; i < 1000; i++ {
		a.Add(float64(i))
	}
	if !a.switched() {
		t.Error("want switched after 1000 distinct values")
	}
}

// TestAdaptiveStaysDistinctOnLowCardinality checks that 100k records of 50 distinct values never switch.
func TestAdaptiveStaysDistinctOnLowCardinality(t *testing.T) {
	a := newAdaptiveTracker(0.5, 64)
	for i := 0; i < 100_000; i++ {
		a.Add(float64(i % 50))
	}
	if a.switched() {
		t.Error("want distinct mode")
	}
}

// TestAdaptiveStaysDistinctWhenHeapsWouldBeBigger checks that heavy repetition keeps distinct mode even past
// the floor.
func TestAdaptiveStaysDistinctWhenHeapsWouldBeBigger(t *testing.T) {
	a := newAdaptiveTracker(0.5, 64)
	for i := 0; i < 10_000; i++ {
		a.Add(float64(i % 64))
	}
	for i := 0; i < 100; i++ {
		a.Add(float64(1000 + i))
	}
	if a.switched() {
		t.Error("want distinct mode")
	}
}

// TestAdaptiveSwitchSplitsCursorCopies checks a switch while the cursor value's copies straddle low and high.
func TestAdaptiveSwitchSplitsCursorCopies(t *testing.T) {
	a, h := newAdaptiveTracker(0.5, 64), newHeapTracker(0.5)
	check := func(i int, v float64) {
		a.Add(v)
		h.Add(v)
		if got, want := a.Quantile(), h.Quantile(); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("add %d: got %v, want %v", i, got, want)
		}
	}
	for i := 0; i < 1200; i++ {
		check(i, float64(i%2))
	}
	for i := 0; i < 500; i++ {
		check(1200+i, float64(i)+2)
	}
	if !a.switched() {
		t.Error("want switched")
	}
}

// TestNewTrackerUsesAdaptive checks that an interior quantile gets the adaptive tracker.
func TestNewTrackerUsesAdaptive(t *testing.T) {
	if _, ok := newTracker(0.5).(*adaptiveTracker); !ok {
		t.Errorf("newTracker(0.5) is %T, want *adaptiveTracker", newTracker(0.5))
	}
}
