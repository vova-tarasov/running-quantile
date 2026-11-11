package main

import (
	"math/rand/v2"
	"runtime"
	"slices"
	"testing"
)

// allocatedBytes returns the total bytes allocated on the heap while f runs (runtime.MemStats.TotalAlloc delta).
func allocatedBytes(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// normalValues returns n seeded normally distributed values.
func normalValues(n int) []float64 {
	r := rand.New(rand.NewPCG(1, 2))
	vals := make([]float64, n)
	for i := range vals {
		vals[i] = r.NormFloat64()
	}
	return vals
}

// checkPopsSorted pops every value from h and fails unless they come out in sorted order with len counting down.
func checkPopsSorted(t *testing.T, h *floatHeap, vals []float64) {
	t.Helper()
	want := slices.Sorted(slices.Values(vals))
	for i, w := range want {
		if h.len() != len(want)-i {
			t.Fatalf("len() = %d, want %d", h.len(), len(want)-i)
		}
		if got := h.top(); got != w {
			t.Fatalf("top() at %d = %v, want %v", i, got, w)
		}
		if got := h.pop(); got != w {
			t.Fatalf("pop() at %d = %v, want %v", i, got, w)
		}
	}
	if h.len() != 0 {
		t.Fatalf("len() = %d after draining, want 0", h.len())
	}
}

// TestFloatHeapPopsInSortedOrder checks that pushed values pop in ascending order across several chunks.
func TestFloatHeapPopsInSortedOrder(t *testing.T) {
	vals := normalValues(30000)
	var h floatHeap
	for _, v := range vals {
		h.push(v)
	}
	checkPopsSorted(t, &h, vals)
}

// TestFloatHeapHeapifyAfterAppendUnordered checks that bulk-appended values pop in order after heapify.
func TestFloatHeapHeapifyAfterAppendUnordered(t *testing.T) {
	vals := normalValues(30000)
	var h floatHeap
	for _, v := range vals {
		h.appendUnordered(v)
	}
	h.heapify()
	checkPopsSorted(t, &h, vals)
}

// TestFloatHeapGrowsWithoutCopying checks that growth allocates close to the stored bytes, with no slice-doubling copies.
func TestFloatHeapGrowsWithoutCopying(t *testing.T) {
	const n = 1 << 20
	var h floatHeap
	got := allocatedBytes(func() {
		for i := range n {
			h.push(float64(i))
		}
	})
	if limit := uint64(n * 8 * 11 / 10); got > limit {
		t.Fatalf("allocated %d bytes for %d values, limit %d", got, n, limit)
	}
}
