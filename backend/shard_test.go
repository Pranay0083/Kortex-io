package main

import (
	"math"
	"strconv"
	"testing"
)

func TestRingDeterministic(t *testing.T) {
	a, b := NewRing(4, 160), NewRing(4, 160)
	for i := 0; i < 5000; i++ {
		k := "code-" + strconv.Itoa(i)
		if a.Get(k) != b.Get(k) || a.Get(k) != a.Get(k) {
			t.Fatalf("key %s mapped inconsistently", k)
		}
	}
}

func TestRingBalance(t *testing.T) {
	const n, keys = 4, 100000
	r := NewRing(n, 160)
	counts := make([]int, n)
	for i := 0; i < keys; i++ {
		counts[r.Get(encodeCode(int64(i)))]++
	}
	mean := float64(keys) / n
	for s, c := range counts {
		if dev := math.Abs(float64(c)-mean) / mean; dev > 0.15 {
			t.Errorf("shard %d holds %d keys (%.1f%% off mean)", s, c, dev*100)
		}
	}
}

func TestRingMinimalMovement(t *testing.T) {
	rep := ringReport(4, 5, 160, 50000, nil)
	// ideal is 1/5 = 20%; naive modulo moves ~80%
	if rep.MovedPct < 12 || rep.MovedPct > 28 {
		t.Fatalf("consistent hashing moved %.1f%% of keys", rep.MovedPct)
	}
	if rep.NaiveMoved < 60 {
		t.Fatalf("naive modulo moved only %.1f%%", rep.NaiveMoved)
	}
}
