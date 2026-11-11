package main

// distinctTracker keeps each distinct value once with a count, so memory follows the number of distinct values.
// cur is the value holding rank k; lower holds distinct values below it (negated), upper those above it.
type distinctTracker struct {
	q        float64
	n        int
	counts   map[float64]int
	lower    floatHeap
	upper    floatHeap
	cur      float64
	curCount int
	below    int
}

// newDistinctTracker returns a distinctTracker for quantile q.
func newDistinctTracker(q float64) *distinctTracker {
	return &distinctTracker{q: q, counts: map[float64]int{}}
}

// distinct returns the number of distinct values seen so far.
func (t *distinctTracker) distinct() int { return len(t.counts) }

// Add records v and moves cur until the copies of cur cover rank k.
func (t *distinctTracker) Add(v float64) {
	t.n++
	c := t.counts[v]
	t.counts[v] = c + 1
	if t.n == 1 {
		t.cur, t.curCount = v, 1
		return
	}
	switch {
	case v < t.cur:
		t.below++
		if c == 0 {
			t.lower.push(-v)
		}
	case v > t.cur:
		if c == 0 {
			t.upper.push(v)
		}
	default:
		t.curCount++
	}
	k, _ := rank(t.n, t.q)
	for t.below > k {
		t.upper.push(t.cur)
		t.cur = -t.lower.pop()
		t.curCount = t.counts[t.cur]
		t.below -= t.curCount
	}
	for t.below+t.curCount <= k {
		t.lower.push(-t.cur)
		t.below += t.curCount
		t.cur = t.upper.pop()
		t.curCount = t.counts[t.cur]
	}
}

// Quantile returns the exact quantile of the values added so far.
func (t *distinctTracker) Quantile() float64 {
	k, f := rank(t.n, t.q)
	if f == 0 {
		return t.cur
	}
	b := t.cur
	if k+1 >= t.below+t.curCount {
		b = t.upper.top()
	}
	return lerp(t.cur, b, f)
}

// toHeaps returns a heapTracker holding the same values, split as heapTracker requires.
func (t *distinctTracker) toHeaps() *heapTracker {
	h := newHeapTracker(t.q)
	k, _ := rank(t.n, t.q)
	for v, c := range t.counts {
		inLow := 0
		switch {
		case v < t.cur:
			inLow = c
		case v == t.cur:
			inLow = k + 1 - t.below
		}
		for i := 0; i < inLow; i++ {
			h.low.appendUnordered(-v)
		}
		for i := inLow; i < c; i++ {
			h.high.appendUnordered(v)
		}
	}
	h.low.heapify()
	h.high.heapify()
	return h
}

// distinctFloor is the number of distinct values below which an adaptiveTracker never switches to heaps.
// It is a judgment call, not tuned by measurement: distinct mode may grow to about 3 MB (2^16 * 46 B) before
// heaps are considered.
const distinctFloor = 1 << 16

// bytesPerDistinct is the memory cost of one distinct value in a distinctTracker, in bytes.
// Measured by adding 1M distinct values to a distinctTracker: 45.85 B per value live (HeapAlloc after
// runtime.GC(), rounded up to 46); TotalAlloc is 83.65 B per value because it counts discarded map tables.
const bytesPerDistinct = 46

// adaptiveTracker starts as a distinctTracker and switches, once and for good, to a heapTracker when storing
// every value costs no more memory than the distinct structure.
type adaptiveTracker struct {
	floor int
	d     *distinctTracker
	h     *heapTracker
}

// newAdaptiveTracker returns an adaptiveTracker for quantile q that may switch once past floor distinct values.
func newAdaptiveTracker(q float64, floor int) *adaptiveTracker {
	return &adaptiveTracker{floor: floor, d: newDistinctTracker(q)}
}

// switched reports whether the tracker has switched to heaps.
func (t *adaptiveTracker) switched() bool { return t.h != nil }

// Add records v, switching to heaps when that no longer costs more memory.
func (t *adaptiveTracker) Add(v float64) {
	if t.h != nil {
		t.h.Add(v)
		return
	}
	t.d.Add(v)
	if t.d.distinct() > t.floor && 8*t.d.n <= bytesPerDistinct*t.d.distinct() {
		t.h = t.d.toHeaps()
		t.d = nil
	}
}

// Quantile returns the exact quantile of the values added so far.
func (t *adaptiveTracker) Quantile() float64 {
	if t.h != nil {
		return t.h.Quantile()
	}
	return t.d.Quantile()
}
