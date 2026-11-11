package main

const (
	chunkBits = 13
	chunkSize = 1 << chunkBits
)

// floatHeap is a binary min-heap of float64 stored in fixed-size chunks, so growing it never copies stored values.
// The zero value is ready to use. Store negated values to get a max-heap.
type floatHeap struct {
	chunks []*[chunkSize]float64
	n      int
}

// len returns the number of values in the heap.
func (h *floatHeap) len() int { return h.n }

// at returns a pointer to the slot with index i.
func (h *floatHeap) at(i int) *float64 {
	return &h.chunks[i>>chunkBits][i&(chunkSize-1)]
}

// top returns the smallest value. The heap must not be empty.
func (h *floatHeap) top() float64 { return *h.at(0) }

// appendUnordered adds v at the end without restoring the heap order; call heapify before top, push or pop.
func (h *floatHeap) appendUnordered(v float64) {
	if h.n == len(h.chunks)*chunkSize {
		h.chunks = append(h.chunks, new([chunkSize]float64))
	}
	*h.at(h.n) = v
	h.n++
}

// push adds v to the heap.
func (h *floatHeap) push(v float64) {
	h.appendUnordered(v)
	h.siftUp(h.n - 1)
}

// pop removes and returns the smallest value. The heap must not be empty.
func (h *floatHeap) pop() float64 {
	min := *h.at(0)
	h.n--
	*h.at(0) = *h.at(h.n)
	h.siftDown(0)
	return min
}

// heapify restores the heap order after appendUnordered calls.
func (h *floatHeap) heapify() {
	for i := h.n/2 - 1; i >= 0; i-- {
		h.siftDown(i)
	}
}

// siftUp moves the value at index i up until its parent is not larger.
func (h *floatHeap) siftUp(i int) {
	v := *h.at(i)
	for i > 0 {
		p := (i - 1) / 2
		pv := *h.at(p)
		if pv <= v {
			break
		}
		*h.at(i) = pv
		i = p
	}
	*h.at(i) = v
}

// siftDown moves the value at index i down until neither child is smaller.
func (h *floatHeap) siftDown(i int) {
	v := *h.at(i)
	for {
		c := 2*i + 1
		if c >= h.n {
			break
		}
		if c+1 < h.n && *h.at(c + 1) < *h.at(c) {
			c++
		}
		if cv := *h.at(c); cv < v {
			*h.at(i) = cv
			i = c
		} else {
			break
		}
	}
	*h.at(i) = v
}
