package main

import (
	"math"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"
)

func snowflakeAt(t time.Time, node, seq int64) int64 {
	return ((t.UnixMilli() - kortexEpoch) << (nodeBits + seqBits)) | (node << seqBits) | seq
}

func TestCodeRoundTrip(t *testing.T) {
	ids := []int64{0, 1, 2, 61, 62, 1 << 36, 1<<58 - 1, 1 << 58, 1<<58 + 1, 1 << 62, math.MaxInt64}
	for _, y := range []int{2025, 2026, 2027, 2028, 2035, 2060, 2094} {
		ids = append(ids, snowflakeAt(time.Date(y, 3, 7, 12, 0, 0, 0, time.UTC), 1, 7))
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		ids = append(ids, rng.Int63())
	}
	for _, id := range ids {
		code := encodeCode(id)
		if len(code) != codeLen {
			t.Fatalf("id %d: code %q has length %d", id, code, len(code))
		}
		got, err := decodeCode(code)
		if err != nil || got != id {
			t.Fatalf("id %d: decode(%q) = %d, %v", id, code, got, err)
		}
	}
}

// The old 58-bit mask made IDs exactly 2^36 ms apart collide.
func TestCodesUniqueAcrossMaskPeriod(t *testing.T) {
	base := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	period := time.Duration(1<<36) * time.Millisecond
	for i := 0; i < 1000; i++ {
		t0 := base.Add(time.Duration(i) * 37 * time.Minute)
		a := snowflakeAt(t0, 1, int64(i%4096))
		b := snowflakeAt(t0.Add(period), 1, int64(i%4096))
		if encodeCode(a) == encodeCode(b) {
			t.Fatalf("ids %d and %d (2^36ms apart) share code %s", a, b, encodeCode(a))
		}
	}
}

func TestBase62DecodeRejects(t *testing.T) {
	for _, s := range []string{"", "abc!", "000000000000", "zzzzzzzzzzz", "LygHa16AHYG"} {
		if _, err := base62Decode(s); err == nil {
			t.Errorf("base62Decode(%q) should fail", s)
		}
	}
	// 2^64-1 is the largest representable value
	if n, err := base62Decode(base62Encode(math.MaxUint64, codeLen)); err != nil || n != math.MaxUint64 {
		t.Fatalf("max uint64: %d, %v", n, err)
	}
}

func TestSnowflakeConcurrentUniqueMonotonic(t *testing.T) {
	sf := NewSnowflake(3)
	const workers, per = 8, 20000
	out := make([][]int64, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			ids := make([]int64, per)
			for i := range ids {
				ids[i] = sf.Next()
			}
			out[w] = ids
		}(w)
	}
	wg.Wait()
	all := make([]int64, 0, workers*per)
	for _, ids := range out {
		for i := 1; i < len(ids); i++ {
			if ids[i] <= ids[i-1] {
				t.Fatalf("not monotonic: %d after %d", ids[i], ids[i-1])
			}
		}
		all = append(all, ids...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	for i := 1; i < len(all); i++ {
		if all[i] == all[i-1] {
			t.Fatalf("duplicate id %d", all[i])
		}
	}
	if p := explodeID(all[0]); p.Node != 3 {
		t.Fatalf("node bits = %d", p.Node)
	}
}
