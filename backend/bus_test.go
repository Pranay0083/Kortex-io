package main

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestBrokerProduceConsume(t *testing.T) {
	var mu sync.Mutex
	got := map[int64]bool{}
	b := NewBroker("t", 3, 10000, func(batch []ClickEvent) {
		mu.Lock()
		for _, e := range batch {
			got[e.TS] = true
		}
		mu.Unlock()
	})
	b.Start()
	defer b.Stop()
	const n = 5000
	for i := 0; i < n; i++ {
		b.Produce(ClickEvent{TS: int64(i), Code: "c" + strconv.Itoa(i%17)})
	}
	if !b.Flush(5 * time.Second) {
		t.Fatal("broker did not drain")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != n {
		t.Fatalf("consumed %d of %d", len(got), n)
	}
	st := b.Stats()
	if st.Consumed != n || st.TotalLag != 0 || st.Lost != 0 {
		t.Fatalf("stats %+v", st)
	}
	b.Stop() // idempotent
}

// Events trimmed by retention before the consumer reaches them are counted,
// so produced == consumed + lost always holds once drained.
func TestBrokerRetentionAccounting(t *testing.T) {
	var mu sync.Mutex
	consumed := 0
	b := NewBroker("t", 2, 100, func(batch []ClickEvent) {
		mu.Lock()
		consumed += len(batch)
		mu.Unlock()
	})
	b.Start()
	defer b.Stop()
	const n = 5000
	for i := 0; i < n; i++ {
		b.Produce(ClickEvent{TS: int64(i), Code: strconv.Itoa(i)})
	}
	if !b.Flush(5 * time.Second) {
		t.Fatal("broker did not drain")
	}
	mu.Lock()
	defer mu.Unlock()
	st := b.Stats()
	if st.Lost == 0 || uint64(consumed)+st.Lost != n || st.Produced != n {
		t.Fatalf("consumed=%d lost=%d produced=%d", consumed, st.Lost, st.Produced)
	}
}

func TestPartitionRetention(t *testing.T) {
	p := &Partition{retention: 10}
	for i := 0; i < 25; i++ {
		p.append(ClickEvent{TS: int64(i)})
		if len(p.log) > 2*p.retention {
			t.Fatalf("log grew to %d", len(p.log))
		}
	}
	if hw := p.highWatermark(); hw != 25 {
		t.Fatalf("high watermark %d", hw)
	}
	// trim at the 21st append dropped offsets 0..10 unconsumed
	if p.baseOff != 11 || p.committed != 11 || p.lost != 11 {
		t.Fatalf("base=%d committed=%d lost=%d", p.baseOff, p.committed, p.lost)
	}
	batch, next := p.poll(100)
	if len(batch) != 14 || batch[0].TS != 11 || batch[len(batch)-1].TS != 24 || next != 25 {
		t.Fatalf("poll returned %d events [%d..%d] next=%d", len(batch), batch[0].TS, batch[len(batch)-1].TS, next)
	}
	p.commit(next)
	p.commit(3) // stale commit must not rewind
	if p.committed != 25 {
		t.Fatalf("committed rewound to %d", p.committed)
	}
	if b, _ := p.poll(10); len(b) != 0 {
		t.Fatalf("expected empty poll, got %d", len(b))
	}
}

func TestBrokerRecentOrderAndFilter(t *testing.T) {
	b := NewBroker("t", 1, 10, func([]ClickEvent) {})
	for i := 0; i < 300; i++ {
		b.pushRecent(ClickEvent{TS: int64(i), UserID: int64(i % 2)})
	}
	r := b.Recent(5, nil)
	if len(r) != 5 || r[0].TS != 299 || r[4].TS != 295 {
		t.Fatalf("recent = %+v", r)
	}
	odd := b.Recent(1000, func(e *ClickEvent) bool { return e.UserID == 1 })
	if len(odd) != 128 {
		t.Fatalf("filtered %d events", len(odd))
	}
	for i, e := range odd {
		if e.UserID != 1 || (i > 0 && e.TS >= odd[i-1].TS) {
			t.Fatalf("bad filtered order at %d: %+v", i, e)
		}
	}
	if len(b.Recent(0, nil)) != 0 {
		t.Fatal("n=0 should return nothing")
	}
}
