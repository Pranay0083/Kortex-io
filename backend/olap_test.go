package main

import (
	"math/rand"
	"reflect"
	"testing"
	"time"
)

var testCodes = []string{"alpha", "bravo", "charlie", "delta"}

// synth inserts n events spread over the last `span`, in several batches.
func synth(o *OLAP, now int64, span time.Duration, n int, seed int64) []ClickEvent {
	rng := rand.New(rand.NewSource(seed))
	all := make([]ClickEvent, 0, n)
	batch := []ClickEvent{}
	for i := 0; i < n; i++ {
		country, city := pickCountry(rng)
		dev, brw, os := pickDevice(rng)
		e := ClickEvent{
			TS: now - rng.Int63n(span.Milliseconds()), Code: testCodes[rng.Intn(len(testCodes))],
			Country: country, City: city, Ref: pickReferrer(rng), Device: dev, Browser: brw, OS: os,
			Bot: rng.Intn(10) == 0,
		}
		all = append(all, e)
		batch = append(batch, e)
		if len(batch) == 500 {
			o.Insert(batch)
			batch = []ClickEvent{}
		}
	}
	o.Insert(batch)
	return all
}

func ids(o *OLAP, codes ...string) []uint32 {
	out := make([]uint32, len(codes))
	for i, c := range codes {
		out[i] = o.CodeID(c)
	}
	return out
}

func total(pts []TSPoint) (n uint64) {
	for _, p := range pts {
		n += p.Count
	}
	return
}

func TestTimeSeriesMVMatchesRaw(t *testing.T) {
	o := NewOLAP(30)
	now := time.Now().UnixMilli()
	synth(o, now, 10*24*time.Hour, 20000, 1)
	o.mergeOnce()
	for _, tc := range []struct {
		gran string
		win  time.Duration
	}{{"minute", time.Hour}, {"minute", 6 * time.Hour}, {"hour", 24 * time.Hour}, {"hour", 7 * 24 * time.Hour}, {"day", 30 * 24 * time.Hour}} {
		for _, c := range [][]uint32{ids(o, "alpha"), ids(o, testCodes...)} {
			from := now - tc.win.Milliseconds()
			mv, _, _ := o.TimeSeriesMV(c, tc.gran, from, now)
			raw, _, _ := o.TimeSeriesRaw(c, tc.gran, from, now)
			if !reflect.DeepEqual(mv, raw) {
				t.Fatalf("%s/%s: mv total %d != raw total %d", tc.gran, tc.win, total(mv), total(raw))
			}
		}
	}
}

func TestBreakdownMVMatchesRaw(t *testing.T) {
	o := NewOLAP(30)
	now := time.Now().UnixMilli()
	synth(o, now, 10*24*time.Hour, 20000, 2)
	for dim := range dimIndex {
		for _, win := range []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, 0} {
			from := int64(0)
			if win > 0 {
				from = now - win.Milliseconds()
			}
			for _, c := range [][]uint32{ids(o, "bravo"), ids(o, testCodes...)} {
				mv, _ := o.BreakdownMV(c, dim, from, now, 0)
				raw, _ := o.BreakdownRaw(c, dim, from, now, 0)
				if !reflect.DeepEqual(mv, raw) {
					t.Fatalf("%s over %s: mv %v != raw %v", dim, win, mv, raw)
				}
			}
		}
	}
	all, _ := o.BreakdownMV(ids(o, testCodes...), "country", 0, now, 0)
	day, _ := o.BreakdownMV(ids(o, testCodes...), "country", now-dayMS, now, 0)
	var na, nd uint64
	for _, r := range all {
		na += r.Count
	}
	for _, r := range day {
		nd += r.Count
	}
	if na != 20000 || nd >= na || nd == 0 {
		t.Fatalf("range not respected: all=%d 24h=%d", na, nd)
	}
	if rows, _ := o.BreakdownMV(ids(o, "alpha"), "bogus", 0, now, 0); len(rows) != 0 {
		t.Fatal("unknown dim should return no rows")
	}
}

func TestSummary(t *testing.T) {
	o := NewOLAP(30)
	now := time.Now().UnixMilli()
	mi, hr := minuteMS, hourMS
	o.Insert([]ClickEvent{
		{TS: now - 2*mi, Code: "alpha", Bot: true},
		{TS: now - 5*mi, Code: "alpha"},
		{TS: now - 3*hr, Code: "alpha"},
		{TS: now - 30*hr, Code: "alpha"},
		{TS: now - 10*mi, Code: "bravo"},
	})
	s := o.Summary(ids(o, "alpha"))
	want := CodeSummary{Total: 4, Bots: 1, First: now - 30*hr, Last: now - 2*mi, Last24: 3, Last1h: 2}
	if s != want {
		t.Fatalf("summary %+v, want %+v", s, want)
	}
	b := o.SummaryBatch(ids(o, "alpha", "bravo", "nobody"))
	if b[0] != want || b[1].Total != 1 || b[2] != (CodeSummary{}) {
		t.Fatalf("batch %+v", b)
	}
	if all := o.Summary(ids(o, "alpha", "bravo")); all.Total != 5 || all.Last1h != 3 {
		t.Fatalf("combined %+v", all)
	}
}

func TestPurge(t *testing.T) {
	o := NewOLAP(30)
	now := time.Now().UnixMilli()
	evs := synth(o, now, 3*24*time.Hour, 5000, 3)
	o.mergeOnce()
	alpha := 0
	for _, e := range evs {
		if e.Code == "alpha" {
			alpha++
		}
	}
	before := o.Summary(ids(o, "bravo"))
	if n := o.Purge("alpha"); n != alpha {
		t.Fatalf("purged %d, want %d", n, alpha)
	}
	if got := o.TotalRows.Load(); got != uint64(len(evs)-alpha) {
		t.Fatalf("total rows %d", got)
	}
	if s := o.Summary(ids(o, "alpha")); s != (CodeSummary{}) {
		t.Fatalf("alpha still has %+v", s)
	}
	raw, _, _ := o.TimeSeriesRaw(ids(o, "alpha"), "day", now-7*dayMS, now)
	if total(raw) != 0 {
		t.Fatal("raw rows survived purge")
	}
	if after := o.Summary(ids(o, "bravo")); after != before {
		t.Fatalf("bravo changed: %+v -> %+v", before, after)
	}
	if o.Stats().MVRows != func() int {
		n := 0
		for _, cm := range o.mv {
			n += cm.rows()
		}
		return n
	}() {
		t.Fatal("mv row accounting drifted")
	}
	if o.Purge("never-seen") != 0 {
		t.Fatal("purging unknown code")
	}
}

func TestRetention(t *testing.T) {
	o := NewOLAP(30)
	now := time.Now().UnixMilli()
	o.Insert([]ClickEvent{
		{TS: now - 45*dayMS, Code: "alpha", Country: "DE"},
		{TS: now - 40*dayMS, Code: "alpha", Country: "DE"},
		{TS: now - dayMS, Code: "alpha", Country: "IN"},
		{TS: now - 50*dayMS, Code: "bravo"},
	})
	o.Retain(now)
	if got := o.TotalRows.Load(); got != 1 {
		t.Fatalf("rows after retention %d", got)
	}
	if s := o.Summary(ids(o, "alpha")); s.Total != 1 {
		t.Fatalf("summary after retention %+v", s)
	}
	if _, ok := o.mv[o.CodeID("bravo")]; ok {
		t.Fatal("fully expired code should be dropped from the MV index")
	}
	rows, _ := o.BreakdownMV(ids(o, "alpha"), "country", 0, now, 0)
	if len(rows) != 1 || rows[0].Key != "IN" {
		t.Fatalf("breakdown after retention %v", rows)
	}
	if st := o.Stats(); len(st.Partitions) != 1 || st.TTLDropped != 3 {
		t.Fatalf("stats %+v", st)
	}
}

func TestMergeKeepsRowsSorted(t *testing.T) {
	o := NewOLAP(0)
	now := time.Now().UnixMilli()
	for i := 0; i < 6; i++ {
		o.Insert([]ClickEvent{{TS: now - int64(i)*1000, Code: "alpha"}, {TS: now - int64(i)*1000 - 500, Code: "bravo"}})
	}
	o.mergeOnce()
	day := dayKey(now)
	if len(o.parts[day]) != 1 && dayKey(now-6000) == day {
		t.Fatalf("expected one merged part, got %d", len(o.parts[day]))
	}
	for _, ps := range o.parts {
		for _, p := range ps {
			for i := 1; i < len(p.TS); i++ {
				if p.TS[i] < p.TS[i-1] {
					t.Fatal("merged part not sorted by ts")
				}
			}
		}
	}
	if o.TotalRows.Load() != 12 {
		t.Fatal("merge changed row count")
	}
}
