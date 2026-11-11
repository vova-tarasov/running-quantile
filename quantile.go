package main

import "math"

// tracker maintains the exact running quantile of the values added so far.
// Quantile must not be called before the first Add.
type tracker interface {
	Add(v float64)
	Quantile() float64
}

// rank returns the type 7 position of quantile q among n sorted values: the integer part k and the fraction f.
// The explicit float64 conversions here and in lerp round each product, so the compiler cannot fuse it with the
// next addition or subtraction into one FMA instruction (it does on arm64) and results are the same everywhere.
func rank(n int, q float64) (k int, f float64) {
	h := float64(float64(n-1) * q)
	k = int(math.Floor(h))
	return k, h - float64(k)
}

// lerp returns a + f*(b-a), falling back to (1-f)*a + f*b when b-a overflows.
func lerp(a, b, f float64) float64 {
	d := b - a
	if math.IsInf(d, 0) {
		return float64((1-f)*a) + float64(f*b)
	}
	return a + float64(f*d)
}

// minTracker tracks the minimum (q = 0) in O(1) memory.
type minTracker struct {
	min float64
	n   int
}

// Add records v.
func (t *minTracker) Add(v float64) {
	if t.n == 0 || v < t.min {
		t.min = v
	}
	t.n++
}

// Quantile returns the minimum seen so far.
func (t *minTracker) Quantile() float64 { return t.min }

// maxTracker tracks the maximum (q = 1) in O(1) memory.
type maxTracker struct {
	max float64
	n   int
}

// Add records v.
func (t *maxTracker) Add(v float64) {
	if t.n == 0 || v > t.max {
		t.max = v
	}
	t.n++
}

// Quantile returns the maximum seen so far.
func (t *maxTracker) Quantile() float64 { return t.max }

// heapTracker keeps the k+1 smallest values in a max-heap (low, stored negated) and the rest in a min-heap (high).
type heapTracker struct {
	q    float64
	low  floatHeap
	high floatHeap
}

// newHeapTracker returns a heapTracker for quantile q.
func newHeapTracker(q float64) *heapTracker { return &heapTracker{q: q} }

// Add records v and rebalances the heaps so low holds exactly the k+1 smallest values.
func (t *heapTracker) Add(v float64) {
	if t.low.len() == 0 || v <= -t.low.top() {
		t.low.push(-v)
	} else {
		t.high.push(v)
	}
	k, _ := rank(t.low.len()+t.high.len(), t.q)
	for t.low.len() > k+1 {
		t.high.push(-t.low.pop())
	}
	for t.low.len() < k+1 {
		t.low.push(-t.high.pop())
	}
}

// Quantile returns the exact quantile of the values added so far.
func (t *heapTracker) Quantile() float64 {
	_, f := rank(t.low.len()+t.high.len(), t.q)
	if f == 0 {
		return -t.low.top()
	}
	return lerp(-t.low.top(), t.high.top(), f)
}

// newTracker returns the tracker best suited to quantile q.
func newTracker(q float64) tracker {
	switch q {
	case 0:
		return &minTracker{}
	case 1:
		return &maxTracker{}
	}
	return newAdaptiveTracker(q, distinctFloor)
}
