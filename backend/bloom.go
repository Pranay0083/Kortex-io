package main

// Bloom filter in front of Postgres for "does this short code exist?".
//
// A Bloom filter is an m-bit array plus k hash functions. Add sets k bits;
// MayContain checks them. If any bit is 0 the key was definitely never
// added, so we skip the database. If all are 1 the key *might* exist (false
// positives happen at a tunable rate), so we ask the shard. That means a
// flood of random or mistyped codes never reaches Postgres.

import (
	"hash/fnv"
	"math"
	"math/bits"
	"sync"
	"sync/atomic"
)

type Bloom struct {
	mu   sync.RWMutex
	bits []uint64 // m bits packed 64 per word
	m    uint64   // number of bits
	k    uint64   // number of hash functions
	n    uint64   // keys added so far

	// counters for the dashboard
	Queries  atomic.Uint64
	Skipped  atomic.Uint64 // filter said "no": DB round trip avoided
	Probes   atomic.Uint64 // filter said "maybe": DB consulted
	FalsePos atomic.Uint64 // DB then said "no"
}

// NewBloom sizes the filter with the standard formulas for `expected` keys
// at false-positive rate p:
//
//	m = -n * ln(p) / (ln 2)^2     bits
//	k = (m / n) * ln 2            hash functions
func NewBloom(expected int, fpRate float64) *Bloom {
	expected = max(expected, 1)
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}
	n := float64(expected)
	m := max(uint64(math.Ceil(-n*math.Log(fpRate)/(math.Ln2*math.Ln2))), 64)
	k := clampU64(uint64(math.Round(float64(m)/n*math.Ln2)), 1, 16)
	return &Bloom{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

func clampU64(v, lo, hi uint64) uint64 { return min(max(v, lo), hi) }

// positions returns the k bit indexes for s. Instead of k separate hash
// functions it uses double hashing (Kirsch-Mitzenmacher): bit_i = h1 + i*h2.
// h2 is forced odd so the steps never get stuck on a short cycle.
func (b *Bloom) positions(s string) []uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	h1 := h.Sum64()
	h.Write([]byte{0x9e})
	h2 := h.Sum64() | 1

	out := make([]uint64, b.k)
	for i := range out {
		out[i] = (h1 + uint64(i)*h2) % b.m
	}
	return out
}

func (b *Bloom) isSet(p uint64) bool { return b.bits[p/64]&(1<<(p%64)) != 0 }

func (b *Bloom) Add(s string) {
	pos := b.positions(s)
	b.mu.Lock()
	for _, p := range pos {
		b.bits[p/64] |= 1 << (p % 64)
	}
	b.n++
	b.mu.Unlock()
}

// MayContain is false only if s was definitely never added.
func (b *Bloom) MayContain(s string) bool {
	pos := b.positions(s)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, p := range pos {
		if !b.isSet(p) {
			return false
		}
	}
	return true
}

// SetBits exposes the bit positions for s so the UI can highlight them.
func (b *Bloom) SetBits(s string) []uint64 { return b.positions(s) }

type BloomStats struct {
	M         uint64  `json:"m"`
	K         uint64  `json:"k"`
	N         uint64  `json:"n"`
	FillRatio float64 `json:"fill_ratio"`
	EstFPR    float64 `json:"est_fpr"`
	Queries   uint64  `json:"queries"`
	DBSkipped uint64  `json:"db_skipped"`
	DBProbes  uint64  `json:"db_probes"`
	FalsePos  uint64  `json:"false_positives"`
	MemoryKB  float64 `json:"memory_kb"`
}

func (b *Bloom) Stats() BloomStats {
	b.mu.RLock()
	set := 0
	for _, w := range b.bits {
		set += bits.OnesCount64(w)
	}
	n, m, k := b.n, b.m, b.k
	b.mu.RUnlock()
	return BloomStats{
		M: m, K: k, N: n,
		FillRatio: float64(set) / float64(m),
		// expected false-positive rate after n inserts: (1 - e^(-kn/m))^k
		EstFPR:    math.Pow(1-math.Exp(-float64(k)*float64(n)/float64(m)), float64(k)),
		Queries:   b.Queries.Load(),
		DBSkipped: b.Skipped.Load(),
		DBProbes:  b.Probes.Load(),
		FalsePos:  b.FalsePos.Load(),
		MemoryKB:  float64(m) / 8 / 1024,
	}
}

// Preview shrinks the bit array to `cells` buckets (set bits per bucket)
// for the UI heat-map.
func (b *Bloom) Preview(cells int) []int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]int, cells)
	per := max(b.m/uint64(cells), 1)
	for i := range out {
		start := uint64(i) * per
		end := min(start+per, b.m)
		for p := start; p < end; p++ {
			if b.isSet(p) {
				out[i]++
			}
		}
	}
	return out
}
