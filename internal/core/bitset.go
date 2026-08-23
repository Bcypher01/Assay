package core

import "math/bits"

// Bitset is a fixed-length bit vector. Signal streams are bitsets rather than
// []bool because 2.6M bars is 325KB here against 2.6MB there — the difference
// between living in cache and streaming from memory.
//
// A Bitset is not safe for concurrent writes. Reads of a completed set are.
type Bitset struct {
	words []uint64
	n     int
}

// NewBitset returns a Bitset of n bits, all clear.
func NewBitset(n int) *Bitset {
	if n < 0 {
		n = 0
	}
	return &Bitset{words: make([]uint64, (n+63)/64), n: n}
}

// Len returns the number of bits.
func (b *Bitset) Len() int { return b.n }

// Set sets bit i. Out-of-range indices are ignored.
func (b *Bitset) Set(i int) {
	if uint(i) >= uint(b.n) {
		return
	}
	b.words[i>>6] |= 1 << uint(i&63)
}

// Clear clears bit i.
func (b *Bitset) Clear(i int) {
	if uint(i) >= uint(b.n) {
		return
	}
	b.words[i>>6] &^= 1 << uint(i&63)
}

// Get reports whether bit i is set. Out-of-range indices report false.
func (b *Bitset) Get(i int) bool {
	if uint(i) >= uint(b.n) {
		return false
	}
	return b.words[i>>6]&(1<<uint(i&63)) != 0
}

// Count returns the number of set bits.
func (b *Bitset) Count() int {
	total := 0
	for _, w := range b.words {
		total += bits.OnesCount64(w)
	}
	return total
}

// Next returns the index of the first set bit at or after from, and whether
// one was found. Iterating a sparse set costs one word scan per gap rather
// than one comparison per bar:
//
//	for i, ok := b.Next(0); ok; i, ok = b.Next(i + 1) { ... }
func (b *Bitset) Next(from int) (int, bool) {
	if from < 0 {
		from = 0
	}
	if from >= b.n {
		return 0, false
	}
	wi := from >> 6
	w := b.words[wi] >> uint(from&63)
	if w != 0 {
		i := from + bits.TrailingZeros64(w)
		if i >= b.n {
			return 0, false
		}
		return i, true
	}
	for wi++; wi < len(b.words); wi++ {
		if b.words[wi] != 0 {
			i := wi<<6 + bits.TrailingZeros64(b.words[wi])
			if i >= b.n {
				return 0, false
			}
			return i, true
		}
	}
	return 0, false
}

// Reset clears every bit without reallocating. Sweeps reuse bitsets across
// thousands of runs; allocating a fresh one per run is the difference between
// a bounded and an unbounded heap.
func (b *Bitset) Reset() {
	for i := range b.words {
		b.words[i] = 0
	}
}

// Resize adapts the set to n bits, reusing the backing array when it is large
// enough. The contents are cleared.
func (b *Bitset) Resize(n int) {
	if n < 0 {
		n = 0
	}
	need := (n + 63) / 64
	if cap(b.words) >= need {
		b.words = b.words[:need]
		b.Reset()
	} else {
		b.words = make([]uint64, need)
	}
	b.n = n
}
