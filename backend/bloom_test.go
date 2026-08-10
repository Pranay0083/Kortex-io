package main

import (
	"strconv"
	"testing"
)

func TestBloomFalsePositiveRate(t *testing.T) {
	for _, target := range []float64{0.01, 0.001} {
		const n, probes = 20000, 200000
		b := NewBloom(n, target)
		for i := 0; i < n; i++ {
			b.Add("member-" + strconv.Itoa(i))
		}
		for i := 0; i < n; i++ {
			if !b.MayContain("member-" + strconv.Itoa(i)) {
				t.Fatalf("false negative for member-%d", i)
			}
		}
		fp := 0
		for i := 0; i < probes; i++ {
			if b.MayContain("outsider-" + strconv.Itoa(i)) {
				fp++
			}
		}
		if got := float64(fp) / probes; got > 2*target {
			t.Errorf("target %.4f: measured FPR %.4f", target, got)
		}
	}
}

func TestBloomDegenerateParams(t *testing.T) {
	b := NewBloom(0, 0)
	b.Add("x")
	if !b.MayContain("x") || b.k < 1 || b.k > 16 {
		t.Fatalf("degenerate bloom: k=%d m=%d", b.k, b.m)
	}
}
