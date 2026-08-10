package main

// In-memory columnar analytics store, modelled on ClickHouse's MergeTree.
//
// Raw data:
//   - Events are partitioned by UTC day. Each insert batch becomes one
//     immutable Part per day, stored column by column (TS[], Code[], ...).
//   - Strings (codes, countries, browsers...) are dictionary-encoded to
//     uint32 ids, like ClickHouse's LowCardinality. Columns stay small and
//     comparisons are cheap.
//   - A background merger folds small parts of the same day into one
//     larger, time-sorted part, so queries scan fewer parts.
//   - Retention drops whole day partitions once they are too old.
//
// Pre-aggregated data ("materialized views", MV):
//   - Every insert also bumps per-code counters: clicks per minute/hour/day,
//     bots per day, and per-hour counts for each dimension value.
//   - Dashboard queries read these counters instead of scanning raw rows.
//     The *Raw query variants scan the columns instead, so the UI can race
//     the two and show they return the same totals.

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	minuteMS = int64(60000)
	hourMS   = 60 * minuteMS
	dayMS    = 24 * hourMS

	// Minute buckets only serve ranges <= 24h, so keep them for 48h.
	minuteRetentionMS = 48 * hourMS
	// Referrers come from clients; once the dictionary is this big, new
	// values become "other" so a flood of junk can't grow memory forever.
	maxDictEntries = 1 << 20

	mergeMinParts   = 4      // merge a day once it has at least this many parts
	mergeMaxPartRow = 400000 // parts bigger than this are left alone
	mergeInterval   = 4 * time.Second
)

// floorTo rounds ts down to a multiple of bucket.
func floorTo(ts, bucket int64) int64 { return ts - ts%bucket }

func dayKey(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02") }

// atomicSub subtracts n from an atomic counter (adding the two's complement).
func atomicSub(c *atomic.Uint64, n int) {
	if n > 0 {
		c.Add(^uint64(n - 1))
	}
}

// --- dictionary encoding -------------------------------------------------

// Dict maps strings <-> dense uint32 ids. Id 0 is the empty string.
type Dict struct {
	mu   sync.RWMutex
	ids  map[string]uint32
	vals []string
}

func NewDict() *Dict { return &Dict{ids: map[string]uint32{}, vals: []string{""}} }

func (d *Dict) Lookup(s string) (uint32, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	v, ok := d.ids[s]
	return v, ok
}

// ID returns s's id, assigning a new one if needed. The read lock covers
// the common case; the second check under the write lock handles two
// goroutines adding the same new string at once.
func (d *Dict) ID(s string) uint32 {
	if v, ok := d.Lookup(s); ok {
		return v
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.ids[s]; ok {
		return v
	}
	id := uint32(len(d.vals))
	d.vals = append(d.vals, s)
	d.ids[s] = id
	return id
}

func (d *Dict) Val(i uint32) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if int(i) < len(d.vals) {
		return d.vals[i]
	}
	return ""
}

func (d *Dict) Size() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.vals)
}

// --- dimensions ----------------------------------------------------------

// The dimensions you can break analytics down by. Each is one column.
const (
	dimCountry = iota
	dimCity
	dimRef
	dimDevice
	dimBrowser
	dimOS
	numDims
)

var dimIndex = map[string]int{
	"country": dimCountry, "city": dimCity, "referrer": dimRef,
	"device": dimDevice, "browser": dimBrowser, "os": dimOS,
}

// eventDims lists an event's dimension values in dim* order.
func eventDims(e *ClickEvent) [numDims]string {
	return [numDims]string{e.Country, e.City, e.Ref, e.Device, e.Browser, e.OS}
}

// --- raw columnar parts --------------------------------------------------

// Part is an immutable chunk of one day's events, stored column by column:
// row i is (TS[i], Code[i], Dims[*][i], Bot[i]).
type Part struct {
	Day   string
	Level int // 0 = fresh insert, 1 = produced by a merge
	MinTS int64
	MaxTS int64
	TS    []int64
	Code  []uint32
	Dims  [numDims][]uint32
	Bot   []bool
}

func (p *Part) Rows() int { return len(p.TS) }

func (p *Part) addRow(ts int64, code uint32, dims [numDims]uint32, bot bool) {
	if len(p.TS) == 0 {
		p.MinTS, p.MaxTS = ts, ts
	}
	p.MinTS = min(p.MinTS, ts)
	p.MaxTS = max(p.MaxTS, ts)
	p.TS = append(p.TS, ts)
	p.Code = append(p.Code, code)
	for d := range p.Dims {
		p.Dims[d] = append(p.Dims[d], dims[d])
	}
	p.Bot = append(p.Bot, bot)
}

// copyRow appends row i of src to p.
func (p *Part) copyRow(src *Part, i int) {
	var dims [numDims]uint32
	for d := range dims {
		dims[d] = src.Dims[d][i]
	}
	p.addRow(src.TS[i], src.Code[i], dims, src.Bot[i])
}

// --- per-code materialized views -----------------------------------------

// codeMV holds every pre-aggregated counter for one short code, so a
// per-link query only touches that link's buckets.
type codeMV struct {
	minute map[int64]uint64                     // minute bucket -> clicks
	hour   map[int64]uint64                     // hour bucket -> clicks
	day    map[int64]uint64                     // day bucket -> clicks
	bots   map[int64]uint64                     // day bucket -> bot clicks
	dims   [numDims]map[int64]map[uint32]uint64 // dim -> hour bucket -> value id -> clicks
	first  int64
	last   int64
}

func newCodeMV(ts int64) *codeMV {
	cm := &codeMV{
		minute: map[int64]uint64{}, hour: map[int64]uint64{}, day: map[int64]uint64{}, bots: map[int64]uint64{},
		first: ts, last: ts,
	}
	for d := range cm.dims {
		cm.dims[d] = map[int64]map[uint32]uint64{}
	}
	return cm
}

// rows counts the map entries this MV holds (for the memory stat).
func (cm *codeMV) rows() int {
	n := len(cm.minute) + len(cm.hour) + len(cm.day) + len(cm.bots)
	for d := range cm.dims {
		for _, values := range cm.dims[d] {
			n += len(values)
		}
	}
	return n
}

func (cm *codeMV) series(gran string) map[int64]uint64 {
	switch gran {
	case "minute":
		return cm.minute
	case "hour":
		return cm.hour
	}
	return cm.day
}

// --- the store -----------------------------------------------------------

type OLAP struct {
	mu        sync.RWMutex
	parts     map[string][]*Part // day -> parts
	dict      *Dict
	mv        map[uint32]*codeMV // code id -> its counters
	mvRows    int
	retention int64 // ms; 0 keeps everything

	TotalRows atomic.Uint64
	Inserts   atomic.Uint64
	Merges    atomic.Uint64
	MVWrites  atomic.Uint64
	Dropped   atomic.Uint64 // rows removed by retention
}

func NewOLAP(retentionDays int) *OLAP {
	return &OLAP{
		parts:     map[string][]*Part{},
		dict:      NewDict(),
		mv:        map[uint32]*codeMV{},
		retention: int64(max(retentionDays, 0)) * dayMS,
	}
}

func (o *OLAP) CodeID(code string) uint32 { return o.dict.ID(code) }

// dimID encodes a dimension value, folding new values into "other" once the
// dictionary is full.
func (o *OLAP) dimID(s string) uint32 {
	if id, ok := o.dict.Lookup(s); ok {
		return id
	}
	if o.dict.Size() >= maxDictEntries {
		return o.dict.ID("other")
	}
	return o.dict.ID(s)
}

// Insert stores a batch: one new immutable part per UTC day in the batch,
// and every row is also added to its code's materialized views.
func (o *OLAP) Insert(batch []ClickEvent) {
	if len(batch) == 0 {
		return
	}
	byDay := map[string][]ClickEvent{}
	for _, e := range batch {
		d := dayKey(e.TS)
		byDay[d] = append(byDay[d], e)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	for day, events := range byDay {
		p := &Part{Day: day}
		for i := range events {
			e := &events[i]
			code := o.dict.ID(e.Code)
			var dims [numDims]uint32
			for d, v := range eventDims(e) {
				dims[d] = o.dimID(v)
			}
			p.addRow(e.TS, code, dims, e.Bot)
			o.updateMV(code, e.TS, dims, e.Bot)
		}
		o.parts[day] = append(o.parts[day], p)
		o.TotalRows.Add(uint64(p.Rows()))
	}
	o.Inserts.Add(1)
}

// incr bumps m[k], tracking how many MV entries exist. Caller holds o.mu.
func (o *OLAP) incr(m map[int64]uint64, k int64) {
	if _, ok := m[k]; !ok {
		o.mvRows++
	}
	m[k]++
}

// updateMV adds one click to every counter for this code. Caller holds o.mu.
func (o *OLAP) updateMV(code uint32, ts int64, dims [numDims]uint32, bot bool) {
	cm := o.mv[code]
	if cm == nil {
		cm = newCodeMV(ts)
		o.mv[code] = cm
	}
	hour, day := floorTo(ts, hourMS), floorTo(ts, dayMS)
	o.incr(cm.minute, floorTo(ts, minuteMS))
	o.incr(cm.hour, hour)
	o.incr(cm.day, day)
	if bot {
		o.incr(cm.bots, day)
	}
	for d, v := range dims {
		values := cm.dims[d][hour]
		if values == nil {
			values = map[uint32]uint64{}
			cm.dims[d][hour] = values
		}
		if _, ok := values[v]; !ok {
			o.mvRows++
		}
		values[v]++
	}
	cm.first = min(cm.first, ts)
	cm.last = max(cm.last, ts)
	o.MVWrites.Add(3 + numDims)
}

// --- background maintenance ----------------------------------------------

// StartMerger runs merges and retention every few seconds, like MergeTree's
// background merges and TTL.
func (o *OLAP) StartMerger() {
	go func() {
		t := time.NewTicker(mergeInterval)
		for range t.C {
			o.mergeOnce()
			o.Retain(time.Now().UnixMilli())
		}
	}()
}

// mergeOnce folds the small parts of each busy day into one time-sorted part.
func (o *OLAP) mergeOnce() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for day, parts := range o.parts {
		if len(parts) < mergeMinParts {
			continue
		}
		var keep, small []*Part
		for _, p := range parts {
			if p.Rows() > mergeMaxPartRow {
				keep = append(keep, p)
			} else {
				small = append(small, p)
			}
		}
		if len(small) < 2 {
			continue
		}
		o.parts[day] = append(keep, mergeParts(day, small))
		o.Merges.Add(1)
	}
}

// mergeParts returns one level-1 part with every row of parts, sorted by
// timestamp (the "primary key"). The sort is stable, so equal timestamps
// keep their insert order.
func mergeParts(day string, parts []*Part) *Part {
	type rowRef struct{ part, row int }
	var refs []rowRef
	for pi, p := range parts {
		for i := range p.TS {
			refs = append(refs, rowRef{pi, i})
		}
	}
	sort.SliceStable(refs, func(a, b int) bool {
		return parts[refs[a].part].TS[refs[a].row] < parts[refs[b].part].TS[refs[b].row]
	})
	merged := &Part{Day: day, Level: 1}
	for _, r := range refs {
		merged.copyRow(parts[r.part], r.row)
	}
	return merged
}

// dropBefore deletes keys older than cutoff and returns how many it removed.
func dropBefore(m map[int64]uint64, cutoff int64) int {
	n := 0
	for k := range m {
		if k < cutoff {
			delete(m, k)
			n++
		}
	}
	return n
}

// Retain drops day partitions and MV buckets older than the retention window.
// The cutoff is day-aligned so raw data and MVs always cover the same days.
// Minute buckets are also trimmed to the last 48h.
func (o *OLAP) Retain(now int64) {
	if o.retention <= 0 {
		return
	}
	cutoff := floorTo(now-o.retention, dayMS)
	cutDay := dayKey(cutoff)

	o.mu.Lock()
	defer o.mu.Unlock()
	for day, parts := range o.parts {
		if day >= cutDay { // "2006-01-02" strings sort like dates
			continue
		}
		for _, p := range parts {
			atomicSub(&o.TotalRows, p.Rows())
			o.Dropped.Add(uint64(p.Rows()))
		}
		delete(o.parts, day)
	}
	for code, cm := range o.mv {
		o.mvRows -= dropBefore(cm.minute, now-minuteRetentionMS)
		o.mvRows -= dropBefore(cm.hour, cutoff) + dropBefore(cm.day, cutoff) + dropBefore(cm.bots, cutoff)
		for d := range cm.dims {
			for hour, values := range cm.dims[d] {
				if hour < cutoff {
					o.mvRows -= len(values)
					delete(cm.dims[d], hour)
				}
			}
		}
		if len(cm.day) == 0 && len(cm.minute) == 0 {
			o.mvRows -= cm.rows()
			delete(o.mv, code)
		}
	}
}

// Purge removes every raw row and MV entry for a code, so a deleted alias
// that gets registered again starts with clean analytics.
func (o *OLAP) Purge(code string) int {
	id, ok := o.dict.Lookup(code)
	if !ok {
		return 0
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if cm := o.mv[id]; cm != nil {
		o.mvRows -= cm.rows()
		delete(o.mv, id)
	}
	removed := 0
	for day, parts := range o.parts {
		var kept []*Part
		for _, p := range parts {
			np, n := withoutCode(p, id)
			removed += n
			if np != nil {
				kept = append(kept, np)
			}
		}
		if len(kept) == 0 {
			delete(o.parts, day)
		} else {
			o.parts[day] = kept
		}
	}
	atomicSub(&o.TotalRows, removed)
	return removed
}

// withoutCode returns p minus the rows for code, and how many rows it
// removed. Parts are immutable, so a changed part is rebuilt; it returns nil
// if nothing is left.
func withoutCode(p *Part, code uint32) (*Part, int) {
	hits := 0
	for _, c := range p.Code {
		if c == code {
			hits++
		}
	}
	switch hits {
	case 0:
		return p, 0
	case p.Rows():
		return nil, hits
	}
	np := &Part{Day: p.Day, Level: p.Level}
	for i, c := range p.Code {
		if c != code {
			np.copyRow(p, i)
		}
	}
	return np, hits
}

// --- queries -------------------------------------------------------------

type TSPoint struct {
	T     int64  `json:"t"`
	Label string `json:"label"`
	Count uint64 `json:"count"`
}

type DimRow struct {
	Key   string  `json:"key"`
	Count uint64  `json:"count"`
	Pct   float64 `json:"pct"`
}

func validGran(g string) bool { return g == "minute" || g == "hour" || g == "day" }

func bucketSize(gran string) int64 {
	switch gran {
	case "minute":
		return minuteMS
	case "hour":
		return hourMS
	}
	return dayMS
}

func codeSet(codes []uint32) map[uint32]bool {
	set := make(map[uint32]bool, len(codes))
	for _, c := range codes {
		set[c] = true
	}
	return set
}

// TimeSeriesMV reads the per-code counters: O(codes x buckets), no matter
// how many raw rows exist. It also returns the time taken and buckets read.
func (o *OLAP) TimeSeriesMV(codes []uint32, gran string, from, to int64) ([]TSPoint, time.Duration, uint64) {
	start := time.Now()
	bs := bucketSize(gran)
	counts := map[int64]uint64{}
	var scanned uint64
	o.mu.RLock()
	for _, c := range codes {
		cm := o.mv[c]
		if cm == nil {
			continue
		}
		series := cm.series(gran)
		for b := floorTo(from, bs); b <= to; b += bs {
			counts[b] += series[b]
			scanned++
		}
	}
	o.mu.RUnlock()
	return fillSeries(counts, from, to, bs, gran), time.Since(start), scanned
}

// TimeSeriesRaw answers the same question by scanning raw rows. Parts whose
// [MinTS, MaxTS] misses the window are skipped entirely.
func (o *OLAP) TimeSeriesRaw(codes []uint32, gran string, from, to int64) ([]TSPoint, time.Duration, uint64) {
	start := time.Now()
	bs := bucketSize(gran)
	want := codeSet(codes)
	counts := map[int64]uint64{}
	var scanned uint64
	floor := floorTo(from, bs)
	o.mu.RLock()
	for _, parts := range o.parts {
		for _, p := range parts {
			if p.MaxTS < floor || p.MinTS > to {
				continue
			}
			for i, ts := range p.TS {
				scanned++
				if ts >= floor && ts <= to && want[p.Code[i]] {
					counts[floorTo(ts, bs)]++
				}
			}
		}
	}
	o.mu.RUnlock()
	return fillSeries(counts, from, to, bs, gran), time.Since(start), scanned
}

// fillSeries turns sparse bucket counts into a full series, including empty
// buckets, so the chart has no gaps.
func fillSeries(counts map[int64]uint64, from, to, bs int64, gran string) []TSPoint {
	layout := map[string]string{"minute": "15:04", "hour": "02 15:00", "day": "Jan 02"}[gran]
	out := []TSPoint{}
	for b := floorTo(from, bs); b <= to; b += bs {
		out = append(out, TSPoint{T: b, Label: time.UnixMilli(b).UTC().Format(layout), Count: counts[b]})
	}
	return out
}

// BreakdownMV returns the top values of a dimension (e.g. top countries).
// The window is hour-aligned, [floor_hour(from), to], so the hourly MV and
// the raw scan answer the same question. from <= 0 means all retained data.
func (o *OLAP) BreakdownMV(codes []uint32, dim string, from, to int64, limit int) ([]DimRow, time.Duration) {
	start := time.Now()
	d, ok := dimIndex[dim]
	if !ok {
		return []DimRow{}, 0
	}
	allTime := from <= 0
	floor := floorTo(from, hourMS)
	windowHours := (to-floor)/hourMS + 1
	counts := map[uint32]uint64{}

	o.mu.RLock()
	for _, c := range codes {
		cm := o.mv[c]
		if cm == nil {
			continue
		}
		byHour := cm.dims[d]
		if !allTime && windowHours < int64(len(byHour)) {
			// Window is smaller than the stored history: look up each hour.
			for h := floor; h <= to; h += hourMS {
				addCounts(counts, byHour[h])
			}
			continue
		}
		// Otherwise walk the stored hours and filter.
		for h, values := range byHour {
			if allTime || (h >= floor && h <= to) {
				addCounts(counts, values)
			}
		}
	}
	o.mu.RUnlock()
	return o.rank(counts, limit), time.Since(start)
}

func addCounts(dst, src map[uint32]uint64) {
	for k, n := range src {
		dst[k] += n
	}
}

// BreakdownRaw is BreakdownMV done as a full column scan.
func (o *OLAP) BreakdownRaw(codes []uint32, dim string, from, to int64, limit int) ([]DimRow, time.Duration) {
	start := time.Now()
	d, ok := dimIndex[dim]
	if !ok {
		return []DimRow{}, 0
	}
	allTime := from <= 0
	floor := floorTo(from, hourMS)
	want := codeSet(codes)
	counts := map[uint32]uint64{}
	o.mu.RLock()
	for _, parts := range o.parts {
		for _, p := range parts {
			if !allTime && (p.MaxTS < floor || p.MinTS > to) {
				continue
			}
			col := p.Dims[d]
			for i, ts := range p.TS {
				if (allTime || (ts >= floor && ts <= to)) && want[p.Code[i]] {
					counts[col[i]]++
				}
			}
		}
	}
	o.mu.RUnlock()
	return o.rank(counts, limit), time.Since(start)
}

// rank decodes value ids, adds percentages and sorts by count (then name).
func (o *OLAP) rank(counts map[uint32]uint64, limit int) []DimRow {
	var total uint64
	for _, n := range counts {
		total += n
	}
	rows := make([]DimRow, 0, len(counts))
	for id, n := range counts {
		pct := 0.0
		if total > 0 {
			pct = float64(n) / float64(total) * 100
		}
		rows = append(rows, DimRow{Key: o.dict.Val(id), Count: n, Pct: pct})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Key < rows[j].Key
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

type CodeSummary struct {
	Total  uint64 `json:"total"`
	Bots   uint64 `json:"bots"`
	First  int64  `json:"first_seen"`
	Last   int64  `json:"last_seen"`
	Last24 uint64 `json:"last_24h"`
	Last1h uint64 `json:"last_1h"`
	Window uint64 `json:"window,omitempty"`
}

// sumBuckets adds the buckets that start inside [from, to].
func sumBuckets(m map[int64]uint64, from, to, bs int64) uint64 {
	var n uint64
	b := floorTo(from, bs)
	if b < from {
		b += bs
	}
	for ; b <= to; b += bs {
		n += m[b]
	}
	return n
}

// summary costs O(days + 24 + 60) per code, no matter how big the MV is.
func (cm *codeMV) summary(now int64) CodeSummary {
	s := CodeSummary{First: cm.first, Last: cm.last}
	for _, v := range cm.day {
		s.Total += v
	}
	for _, v := range cm.bots {
		s.Bots += v
	}
	s.Last24 = sumBuckets(cm.hour, now-dayMS, now, hourMS)
	s.Last1h = sumBuckets(cm.minute, now-hourMS, now, minuteMS)
	return s
}

func (s *CodeSummary) merge(o CodeSummary) {
	s.Total += o.Total
	s.Bots += o.Bots
	s.Last24 += o.Last24
	s.Last1h += o.Last1h
	if o.First != 0 && (s.First == 0 || o.First < s.First) {
		s.First = o.First
	}
	s.Last = max(s.Last, o.Last)
}

// Summary adds up the summaries of several codes.
func (o *OLAP) Summary(codes []uint32) CodeSummary {
	var s CodeSummary
	for _, cs := range o.SummaryBatch(codes) {
		s.merge(cs)
	}
	return s
}

// SummaryBatch returns one summary per code, in the same order as codes.
func (o *OLAP) SummaryBatch(codes []uint32) []CodeSummary {
	now := time.Now().UnixMilli()
	out := make([]CodeSummary, len(codes))
	o.mu.RLock()
	defer o.mu.RUnlock()
	for i, c := range codes {
		if cm := o.mv[c]; cm != nil {
			out[i] = cm.summary(now)
		}
	}
	return out
}

// --- stats ---------------------------------------------------------------

type PartitionInfo struct {
	Day   string `json:"day"`
	Parts int    `json:"parts"`
	Rows  int    `json:"rows"`
}

type OLAPStats struct {
	TotalRows     uint64          `json:"total_rows"`
	Partitions    []PartitionInfo `json:"partitions"`
	PartCount     int             `json:"part_count"`
	Merges        uint64          `json:"merges"`
	Inserts       uint64          `json:"inserts"`
	MVRows        int             `json:"mv_rows"`
	DictEntries   int             `json:"dict_entries"`
	MemMB         float64         `json:"approx_mem_mb"`
	RetentionDays int64           `json:"retention_days"`
	TTLDropped    uint64          `json:"ttl_dropped_rows"`
}

// bytesPerRow is roughly one row's raw size: 8 (ts) + 4 (code) + 6*4 (dims) + 1 (bot) + slack.
const bytesPerRow = 41

func (o *OLAP) Stats() OLAPStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	st := OLAPStats{
		TotalRows: o.TotalRows.Load(), Merges: o.Merges.Load(), Inserts: o.Inserts.Load(),
		DictEntries: o.dict.Size(), MVRows: o.mvRows, RetentionDays: o.retention / dayMS, TTLDropped: o.Dropped.Load(),
	}
	days := make([]string, 0, len(o.parts))
	for d := range o.parts {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days {
		rows := 0
		for _, p := range o.parts[d] {
			rows += p.Rows()
		}
		st.Partitions = append(st.Partitions, PartitionInfo{Day: d, Parts: len(o.parts[d]), Rows: rows})
		st.PartCount += len(o.parts[d])
	}
	st.MemMB = float64(st.TotalRows) * bytesPerRow / 1024 / 1024
	return st
}
