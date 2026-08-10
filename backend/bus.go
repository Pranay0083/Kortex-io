package main

// In-process event bus for click events, built like a tiny Kafka topic.
//
//	redirect handler --Produce--> ingress channel --dispatch--> partition[hash(code) % P]
//	                                                                  |
//	                                       one consumer goroutine per partition
//	                                                                  |
//	                                                     sink (OLAP.Insert) in batches
//
// Why: a redirect must answer in about a millisecond, so it must not wait on
// analytics. Produce is a non-blocking channel send; if the buffer is full
// the event is dropped and counted, and the redirect still succeeds.
// Routing by code keeps one link's events ordered within one partition.

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ClickEvent is the record produced on every redirect.
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
	Status  int    `json:"status"`
	Hit     bool   `json:"cache_hit"`
}

const (
	ingressBuffer     = 200000 // events waiting to be routed to a partition
	recentSize        = 256    // ring buffer for the live event stream
	pollInterval      = 50 * time.Millisecond
	batchSize         = 4000
	maxBatchesPerTick = 4
)

// Partition is an append-only log with offsets, like a Kafka partition.
// Offsets keep increasing even after old entries are trimmed: log[0] has
// offset baseOff.
type Partition struct {
	mu        sync.Mutex
	log       []ClickEvent
	baseOff   int64 // offset of log[0]
	committed int64 // next offset the consumer will read
	retention int   // entries to keep after a trim
	polled    int64 // end offset of the batch last handed to the consumer
	lost      uint64
}

// end is the high watermark: the offset the next appended event will get.
// Caller holds p.mu.
func (p *Partition) end() int64 { return p.baseOff + int64(len(p.log)) }

func (p *Partition) highWatermark() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.end()
}

// append adds an event. The log may grow to 2x retention; then the oldest
// part is dropped in one copy, so each append is O(1) on average. Events
// the consumer never saw are counted as lost.
func (p *Partition) append(e ClickEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, e)
	if len(p.log) <= 2*p.retention {
		return
	}
	drop := len(p.log) - p.retention
	kept := make([]ClickEvent, p.retention, 2*p.retention+1)
	copy(kept, p.log[drop:])
	p.log = kept
	p.baseOff += int64(drop)
	if seen := max(p.committed, p.polled); seen < p.baseOff {
		p.lost += uint64(p.baseOff - seen)
	}
	p.committed = max(p.committed, p.baseOff)
}

// poll copies up to limit events starting at the committed offset and
// returns them with the offset to commit once they are processed.
func (p *Partition) poll(limit int) ([]ClickEvent, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.committed >= p.end() {
		return nil, p.committed
	}
	start := p.committed - p.baseOff
	stop := min(start+int64(limit), int64(len(p.log)))
	batch := make([]ClickEvent, stop-start)
	copy(batch, p.log[start:stop])
	p.polled = p.baseOff + stop
	return batch, p.polled
}

// commit never moves backwards: a trim may already have moved committed past
// the batch the consumer just finished.
func (p *Partition) commit(off int64) {
	p.mu.Lock()
	p.committed = max(p.committed, off)
	p.mu.Unlock()
}

type Broker struct {
	Topic   string
	parts   []*Partition
	ingress chan ClickEvent
	sink    func([]ClickEvent)

	Produced atomic.Uint64
	Dropped  atomic.Uint64 // ingress buffer was full
	Consumed atomic.Uint64
	Batches  atomic.Uint64

	recentMu sync.Mutex
	recent   [recentSize]ClickEvent // ring buffer of the latest events
	recentN  uint64                 // total events ever pushed to recent

	stop     chan struct{}
	stopOnce sync.Once
}

func NewBroker(topic string, partitions, retention int, sink func([]ClickEvent)) *Broker {
	b := &Broker{
		Topic:   topic,
		ingress: make(chan ClickEvent, ingressBuffer),
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
	for _, p := range b.parts {
		go b.consume(p)
	}
}

func (b *Broker) Stop() { b.stopOnce.Do(func() { close(b.stop) }) }

func (b *Broker) stopped() bool {
	select {
	case <-b.stop:
		return true
	default:
		return false
	}
}

// dispatch routes each event to its partition by hash of the short code.
func (b *Broker) dispatch() {
	for {
		select {
		case <-b.stop:
			return
		case e := <-b.ingress:
			b.parts[hash32(e.Code)%uint32(len(b.parts))].append(e)
			b.pushRecent(e)
		}
	}
}

// consume is one partition's consumer loop: poll a batch, hand it to the
// sink, then commit. It does a bounded amount of work per tick, because one
// huge insert would hold the OLAP write lock long enough to slow redirects.
func (b *Broker) consume(p *Partition) {
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-tick.C:
		}
		for i := 0; i < maxBatchesPerTick; i++ {
			batch, next := p.poll(batchSize)
			if len(batch) == 0 {
				break
			}
			if b.stopped() {
				return
			}
			b.sink(batch)
			p.commit(next)
			b.Consumed.Add(uint64(len(batch)))
			b.Batches.Add(1)
			runtime.Gosched() // let request goroutines run between batches
		}
	}
}

func (b *Broker) pushRecent(e ClickEvent) {
	b.recentMu.Lock()
	b.recent[b.recentN%recentSize] = e
	b.recentN++
	b.recentMu.Unlock()
}

// Recent returns up to n of the latest events, newest first, that satisfy
// keep (nil keeps everything).
func (b *Broker) Recent(n int, keep func(*ClickEvent) bool) []ClickEvent {
	b.recentMu.Lock()
	defer b.recentMu.Unlock()
	available := min(b.recentN, recentSize)
	out := make([]ClickEvent, 0, min(uint64(max(n, 0)), available))
	for i := uint64(0); i < available && len(out) < n; i++ {
		e := &b.recent[(b.recentN-1-i)%recentSize]
		if keep == nil || keep(e) {
			out = append(out, *e)
		}
	}
	return out
}

type PartitionStat struct {
	ID        int    `json:"id"`
	HighWater int64  `json:"high_watermark"`
	Committed int64  `json:"committed"`
	Lag       int64  `json:"lag"`
	Retained  int    `json:"retained"`
	Lost      uint64 `json:"retention_dropped"`
}

type BrokerStats struct {
	Topic      string          `json:"topic"`
	Partitions []PartitionStat `json:"partitions"`
	Produced   uint64          `json:"produced"`
	Consumed   uint64          `json:"consumed"`
	Dropped    uint64          `json:"dropped"`
	Batches    uint64          `json:"batches"`
	Lost       uint64          `json:"retention_dropped"`
	TotalLag   int64           `json:"total_lag"`
	QueueDepth int             `json:"queue_depth"`
}

func (b *Broker) Stats() BrokerStats {
	s := BrokerStats{
		Topic: b.Topic, Produced: b.Produced.Load(), Consumed: b.Consumed.Load(),
		Dropped: b.Dropped.Load(), Batches: b.Batches.Load(), QueueDepth: len(b.ingress),
	}
	for i, p := range b.parts {
		p.mu.Lock()
		st := PartitionStat{
			ID: i, HighWater: p.end(), Committed: p.committed,
			Lag: p.end() - p.committed, Retained: len(p.log), Lost: p.lost,
		}
		p.mu.Unlock()
		s.Partitions = append(s.Partitions, st)
		s.TotalLag += st.Lag
		s.Lost += st.Lost
	}
	return s
}

// Flush waits until every queued event has been consumed, or the timeout
// passes. Used by benchmarks and on shutdown.
func (b *Broker) Flush(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(b.ingress) == 0 && b.Stats().TotalLag == 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
