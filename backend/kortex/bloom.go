package main

import (
	"hash/fnv"
	"math"
	"sync"
	"sync/atomic"
)

// Classic bloom filter sitting in front of Postgres for custom-alias
// existence checks. "definitely not present" short-circuits the DB round trip.
type Bloom struct {
	mu      sync.RWMutex
	bits    []uint64
	m       uint64
	k       uint64
	n       uint64
	Queries atomic.Uint64
	Skipped atomic.Uint64 // DB round trips avoided
	Probes  atomic.Uint64 // bloom said maybe -> DB consulted
	FalsePos atomic.Uint64
}

func NewBloom(expected int, fpRate float64) *Bloom {
	m := uint64(math.Ceil(-float64(expected) * math.Log(fpRate) / (math.Ln2 * math.Ln2)))
	if m < 64 {
		m = 64
	}
	k := uint64(math.Round(float64(m) / float64(expected) * math.Ln2))
	if k < 1 {
		k = 1
	}
	if k > 16 {
		k = 16
	}
	return &Bloom{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

func (b *Bloom) hashes(s string) (uint64, uint64) {
	h := fnv.New64a()
	h.Write([]byte(s))
	h1 := h.Sum64()
	h.Write([]byte{0x9e})
	h2 := h.Sum64() | 1
	return h1, h2
}

func (b *Bloom) Add(s string) {
	h1, h2 := b.hashes(s)
	b.mu.Lock()
	for i := uint64(0); i < b.k; i++ {
		p := (h1 + i*h2) % b.m
		b.bits[p/64] |= 1 << (p % 64)
	}
	b.n++
	b.mu.Unlock()
}

func (b *Bloom) MayContain(s string) bool {
	h1, h2 := b.hashes(s)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := uint64(0); i < b.k; i++ {
		p := (h1 + i*h2) % b.m
		if b.bits[p/64]&(1<<(p%64)) == 0 {
			return false
		}
	}
	return true
}

func (b *Bloom) SetBits(s string) []uint64 {
	h1, h2 := b.hashes(s)
	out := make([]uint64, 0, b.k)
	for i := uint64(0); i < b.k; i++ {
		out = append(out, (h1+i*h2)%b.m)
	}
	return out
}

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
		set += popcount(w)
	}
	n, m, k := b.n, b.m, b.k
	b.mu.RUnlock()
	fill := float64(set) / float64(m)
	return BloomStats{
		M: m, K: k, N: n,
		FillRatio: fill,
		EstFPR:    math.Pow(1-math.Exp(-float64(k)*float64(n)/float64(m)), float64(k)),
		Queries:   b.Queries.Load(),
		DBSkipped: b.Skipped.Load(),
		DBProbes:  b.Probes.Load(),
		FalsePos:  b.FalsePos.Load(),
		MemoryKB:  float64(m) / 8 / 1024,
	}
}

// Bitmap preview for the UI (downsampled to `cells` buckets).
func (b *Bloom) Preview(cells int) []int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]int, cells)
	per := b.m / uint64(cells)
	if per == 0 {
		per = 1
	}
	for i := 0; i < cells; i++ {
		start := uint64(i) * per
		end := start + per
		if end > b.m {
			end = b.m
		}
		c := 0
		for p := start; p < end; p++ {
			if b.bits[p/64]&(1<<(p%64)) != 0 {
				c++
			}
		}
		out[i] = c
	}
	return out
}

func popcount(x uint64) int {
	c := 0
	for x != 0 {
		x &= x - 1
		c++
	}
	return c
}
