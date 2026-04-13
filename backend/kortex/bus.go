package main

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ClickEvent is the wire record produced on every redirect.
type ClickEvent struct {
	TS      int64  `json:"ts"`
	Code    string `json:"code"`
	UserID  int64  `json:"user_id,string"`
	Country string `json:"country"`
	City    string `json:"city"`
	Ref     string `json:"referrer"`
	Device  string `json:"device"`
	Browser string `json:"browser"`
	OS      string `json:"os"`
	Bot     bool   `json:"bot"`
	IP      string `json:"ip"`
	Status  int    `json:"status"`
	Hit     bool   `json:"cache_hit"`
}

// Partition is an append-only log segment with monotonic offsets, the same
// contract Kafka gives a consumer group.
type Partition struct {
	mu        sync.Mutex
	log       []ClickEvent
	baseOff   int64 // offset of log[0]
	committed int64
	retention int
}

func (p *Partition) append(e ClickEvent) {
	p.mu.Lock()
	p.log = append(p.log, e)
	if len(p.log) > p.retention {
		drop := len(p.log) - p.retention
		p.log = append([]ClickEvent(nil), p.log[drop:]...)
		p.baseOff += int64(drop)
		if p.committed < p.baseOff {
			p.committed = p.baseOff
		}
	}
	p.mu.Unlock()
}

func (p *Partition) highWatermark() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.baseOff + int64(len(p.log))
}

func (p *Partition) poll(max int) ([]ClickEvent, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	hw := p.baseOff + int64(len(p.log))
	if p.committed >= hw {
		return nil, p.committed
	}
	start := p.committed - p.baseOff
	end := start + int64(max)
	if end > int64(len(p.log)) {
		end = int64(len(p.log))
	}
	batch := make([]ClickEvent, end-start)
	copy(batch, p.log[start:end])
	return batch, p.baseOff + end
}

func (p *Partition) commit(off int64) {
	p.mu.Lock()
	p.committed = off
	p.mu.Unlock()
}

type Broker struct {
	Topic      string
	parts      []*Partition
	ingress    chan ClickEvent
	Produced   atomic.Uint64
	Dropped    atomic.Uint64
	Consumed   atomic.Uint64
	Batches    atomic.Uint64
	sink       func([]ClickEvent)
	recentMu   sync.Mutex
	recent     [256]ClickEvent
	recentN    uint64
	stop       chan struct{}
}

func NewBroker(topic string, partitions, retention int, sink func([]ClickEvent)) *Broker {
	b := &Broker{
		ingress: make(chan ClickEvent, 200000),
		Topic:   topic,
		sink:    sink,
		stop:    make(chan struct{}),
	}
	for i := 0; i < partitions; i++ {
		b.parts = append(b.parts, &Partition{retention: retention})
	}
	return b
}

// Produce never blocks the redirect path. Full buffer => counted drop.
func (b *Broker) Produce(e ClickEvent) {
	select {
	case b.ingress <- e:
		b.Produced.Add(1)
	default:
		b.Dropped.Add(1)
	}
}

func (b *Broker) Start() {
	go b.dispatch()
	for i := range b.parts {
		go b.consume(i)
	}
}

func (b *Broker) dispatch() {
	for {
		select {
		case <-b.stop:
			return
		case e := <-b.ingress:
			idx := int(hash32(e.Code)) % len(b.parts)
			b.parts[idx].append(e)
			b.pushRecent(e)
		}
	}
}

func (b *Broker) pushRecent(e ClickEvent) {
	b.recentMu.Lock()
	b.recent[b.recentN%256] = e
	b.recentN++
	b.recentMu.Unlock()
}

func (b *Broker) Recent(n int) []ClickEvent {
	b.recentMu.Lock()
	defer b.recentMu.Unlock()
	total := int(b.recentN)
	if total > 256 {
		total = 256
	}
	if n > total {
		n = total
	}
	out := make([]ClickEvent, n)
	for i := 0; i < n; i++ {
		out[i] = b.recent[(b.recentN-uint64(i)-1)%256]
	}
	return out
}

func (b *Broker) consume(idx int) {
	p := b.parts[idx]
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-tick.C:
			// Bounded work per tick: a single huge insert would hold the
			// columnar write lock long enough to show up in redirect p99.
			for i := 0; i < 4; i++ {
				batch, next := p.poll(4000)
				if len(batch) == 0 {
					break
				}
				b.sink(batch)
				p.commit(next)
				b.Consumed.Add(uint64(len(batch)))
				b.Batches.Add(1)
				runtime.Gosched()
			}
		}
	}
}

type PartitionStat struct {
	ID        int   `json:"id"`
	HighWater int64 `json:"high_watermark"`
	Committed int64 `json:"committed"`
	Lag       int64 `json:"lag"`
	Retained  int   `json:"retained"`
}

type BrokerStats struct {
	Topic      string          `json:"topic"`
	Partitions []PartitionStat `json:"partitions"`
	Produced   uint64          `json:"produced"`
	Consumed   uint64          `json:"consumed"`
	Dropped    uint64          `json:"dropped"`
	Batches    uint64          `json:"batches"`
	TotalLag   int64           `json:"total_lag"`
	QueueDepth int             `json:"queue_depth"`
}

func (b *Broker) Stats() BrokerStats {
	s := BrokerStats{Topic: b.Topic, Produced: b.Produced.Load(), Consumed: b.Consumed.Load(),
		Dropped: b.Dropped.Load(), Batches: b.Batches.Load(), QueueDepth: len(b.ingress)}
	for i, p := range b.parts {
		p.mu.Lock()
		hw := p.baseOff + int64(len(p.log))
		st := PartitionStat{ID: i, HighWater: hw, Committed: p.committed, Lag: hw - p.committed, Retained: len(p.log)}
		p.mu.Unlock()
		s.Partitions = append(s.Partitions, st)
		s.TotalLag += st.Lag
	}
	return s
}

// Flush blocks until every partition is fully consumed (used by benchmarks).
func (b *Broker) Flush(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(b.ingress) == 0 {
			done := true
			for _, p := range b.parts {
				p.mu.Lock()
				if p.committed < p.baseOff+int64(len(p.log)) {
					done = false
				}
				p.mu.Unlock()
			}
			if done {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
