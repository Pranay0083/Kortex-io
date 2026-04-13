package main

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Columnar, day-partitioned store modelled on a ClickHouse MergeTree:
// immutable parts per insert, background merge, LowCardinality dictionary
// encoding, and incrementally-maintained materialized views.

type Dict struct {
	mu   sync.RWMutex
	ids  map[string]uint32
	vals []string
}

func NewDict() *Dict { return &Dict{ids: map[string]uint32{}, vals: []string{""}} }

func (d *Dict) ID(s string) uint32 {
	d.mu.RLock()
	if v, ok := d.ids[s]; ok {
		d.mu.RUnlock()
		return v
	}
	d.mu.RUnlock()
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

type Part struct {
	Day     string
	Level   int
	MinTS   int64
	MaxTS   int64
	TS      []int64
	Code    []uint32
	Country []uint32
	City    []uint32
	Ref     []uint32
	Device  []uint32
	Browser []uint32
	OS      []uint32
	Bot     []bool
}

func (p *Part) Rows() int { return len(p.TS) }

type mvKey struct {
	Code   uint32
	Bucket int64
}

type dimKey struct {
	Code uint32
	Val  uint32
}

type OLAP struct {
	mu    sync.RWMutex
	parts map[string][]*Part
	dict  *Dict

	mvMinute map[mvKey]uint64
	mvHour   map[mvKey]uint64
	mvDay    map[mvKey]uint64
	mvGeo    map[dimKey]uint64
	mvCity   map[dimKey]uint64
	mvRef    map[dimKey]uint64
	mvDevice map[dimKey]uint64
	mvBrowse map[dimKey]uint64
	mvOS     map[dimKey]uint64
	mvBots   map[uint32]uint64
	mvFirst  map[uint32]int64
	mvLast   map[uint32]int64

	TotalRows atomic.Uint64
	Inserts   atomic.Uint64
	Merges    atomic.Uint64
	MVWrites  atomic.Uint64
}

func NewOLAP() *OLAP {
	return &OLAP{
		parts:    map[string][]*Part{},
		dict:     NewDict(),
		mvMinute: map[mvKey]uint64{},
		mvHour:   map[mvKey]uint64{},
		mvDay:    map[mvKey]uint64{},
		mvGeo:    map[dimKey]uint64{},
		mvCity:   map[dimKey]uint64{},
		mvRef:    map[dimKey]uint64{},
		mvDevice: map[dimKey]uint64{},
		mvBrowse: map[dimKey]uint64{},
		mvOS:     map[dimKey]uint64{},
		mvBots:   map[uint32]uint64{},
		mvFirst:  map[uint32]int64{},
		mvLast:   map[uint32]int64{},
	}
}

func dayKey(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

func (o *OLAP) CodeID(code string) uint32 { return o.dict.ID(code) }

// Insert writes one immutable part per batch and fans out into the MVs.
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
	for day, evs := range byDay {
		p := &Part{Day: day, Level: 0, MinTS: evs[0].TS, MaxTS: evs[0].TS}
		for _, e := range evs {
			cid := o.dict.ID(e.Code)
			p.TS = append(p.TS, e.TS)
			p.Code = append(p.Code, cid)
			p.Country = append(p.Country, o.dict.ID(e.Country))
			p.City = append(p.City, o.dict.ID(e.City))
			p.Ref = append(p.Ref, o.dict.ID(e.Ref))
			p.Device = append(p.Device, o.dict.ID(e.Device))
			p.Browser = append(p.Browser, o.dict.ID(e.Browser))
			p.OS = append(p.OS, o.dict.ID(e.OS))
			p.Bot = append(p.Bot, e.Bot)
			if e.TS < p.MinTS {
				p.MinTS = e.TS
			}
			if e.TS > p.MaxTS {
				p.MaxTS = e.TS
			}
			o.applyMV(cid, e)
		}
		o.parts[day] = append(o.parts[day], p)
		o.TotalRows.Add(uint64(p.Rows()))
	}
	o.Inserts.Add(1)
}

func (o *OLAP) applyMV(cid uint32, e ClickEvent) {
	min := e.TS - e.TS%60000
	hour := e.TS - e.TS%3600000
	day := e.TS - e.TS%86400000
	o.mvMinute[mvKey{cid, min}]++
	o.mvHour[mvKey{cid, hour}]++
	o.mvDay[mvKey{cid, day}]++
	o.mvGeo[dimKey{cid, o.dict.ID(e.Country)}]++
	o.mvCity[dimKey{cid, o.dict.ID(e.City)}]++
	o.mvRef[dimKey{cid, o.dict.ID(e.Ref)}]++
	o.mvDevice[dimKey{cid, o.dict.ID(e.Device)}]++
	o.mvBrowse[dimKey{cid, o.dict.ID(e.Browser)}]++
	o.mvOS[dimKey{cid, o.dict.ID(e.OS)}]++
	if e.Bot {
		o.mvBots[cid]++
	}
	if f, ok := o.mvFirst[cid]; !ok || e.TS < f {
		o.mvFirst[cid] = e.TS
	}
	if l, ok := o.mvLast[cid]; !ok || e.TS > l {
		o.mvLast[cid] = e.TS
	}
	o.MVWrites.Add(9)
}

// StartMerger emulates MergeTree background merges: small parts within a
// day partition get folded into larger level-N parts.
func (o *OLAP) StartMerger() {
	go func() {
		t := time.NewTicker(4 * time.Second)
		for range t.C {
			o.mergeOnce()
		}
	}()
}

func (o *OLAP) mergeOnce() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for day, ps := range o.parts {
		if len(ps) < 4 {
			continue
		}
		merged := &Part{Day: day, Level: 1, MinTS: ps[0].MinTS, MaxTS: ps[0].MaxTS}
		keep := []*Part{}
		count := 0
		for _, p := range ps {
			if p.Rows() > 400000 {
				keep = append(keep, p)
				continue
			}
			merged.TS = append(merged.TS, p.TS...)
			merged.Code = append(merged.Code, p.Code...)
			merged.Country = append(merged.Country, p.Country...)
			merged.City = append(merged.City, p.City...)
			merged.Ref = append(merged.Ref, p.Ref...)
			merged.Device = append(merged.Device, p.Device...)
			merged.Browser = append(merged.Browser, p.Browser...)
			merged.OS = append(merged.OS, p.OS...)
			merged.Bot = append(merged.Bot, p.Bot...)
			if p.MinTS < merged.MinTS {
				merged.MinTS = p.MinTS
			}
			if p.MaxTS > merged.MaxTS {
				merged.MaxTS = p.MaxTS
			}
			count++
		}
		if count < 2 {
			continue
		}
		// keep primary-key order inside the merged part
		idx := make([]int, len(merged.TS))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return merged.TS[idx[a]] < merged.TS[idx[b]] })
		sorted := &Part{Day: day, Level: 1, MinTS: merged.MinTS, MaxTS: merged.MaxTS}
		for _, i := range idx {
			sorted.TS = append(sorted.TS, merged.TS[i])
			sorted.Code = append(sorted.Code, merged.Code[i])
			sorted.Country = append(sorted.Country, merged.Country[i])
			sorted.City = append(sorted.City, merged.City[i])
			sorted.Ref = append(sorted.Ref, merged.Ref[i])
			sorted.Device = append(sorted.Device, merged.Device[i])
			sorted.Browser = append(sorted.Browser, merged.Browser[i])
			sorted.OS = append(sorted.OS, merged.OS[i])
			sorted.Bot = append(sorted.Bot, merged.Bot[i])
		}
		o.parts[day] = append(keep, sorted)
		o.Merges.Add(1)
	}
}

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

func bucketSize(gran string) int64 {
	switch gran {
	case "minute":
		return 60000
	case "hour":
		return 3600000
	default:
		return 86400000
	}
}

// TimeSeriesMV reads the pre-aggregated rollup: O(buckets).
func (o *OLAP) TimeSeriesMV(codes []uint32, gran string, from, to int64) ([]TSPoint, time.Duration, uint64) {
	start := time.Now()
	bs := bucketSize(gran)
	var mv map[mvKey]uint64
	o.mu.RLock()
	switch gran {
	case "minute":
		mv = o.mvMinute
	case "hour":
		mv = o.mvHour
	default:
		mv = o.mvDay
	}
	acc := map[int64]uint64{}
	var scanned uint64
	for _, c := range codes {
		for b := from - from%bs; b <= to; b += bs {
			if v, ok := mv[mvKey{c, b}]; ok {
				acc[b] += v
			}
			scanned++
		}
	}
	o.mu.RUnlock()
	return fillSeries(acc, from, to, bs, gran), time.Since(start), scanned
}

// TimeSeriesRaw does the honest full scan over the raw columns.
func (o *OLAP) TimeSeriesRaw(codes []uint32, gran string, from, to int64) ([]TSPoint, time.Duration, uint64) {
	start := time.Now()
	bs := bucketSize(gran)
	set := map[uint32]bool{}
	for _, c := range codes {
		set[c] = true
	}
	acc := map[int64]uint64{}
	var scanned uint64
	floor := from - from%bs
	o.mu.RLock()
	for _, ps := range o.parts {
		for _, p := range ps {
			if p.MaxTS < floor || p.MinTS > to {
				continue
			}
			for i := 0; i < len(p.TS); i++ {
				scanned++
				if p.TS[i] < floor || p.TS[i] > to {
					continue
				}
				if !set[p.Code[i]] {
					continue
				}
				acc[p.TS[i]-p.TS[i]%bs]++
			}
		}
	}
	o.mu.RUnlock()
	return fillSeries(acc, from, to, bs, gran), time.Since(start), scanned
}

func fillSeries(acc map[int64]uint64, from, to, bs int64, gran string) []TSPoint {
	layout := "15:04"
	if gran == "day" {
		layout = "Jan 02"
	} else if gran == "hour" {
		layout = "02 15:00"
	}
	out := []TSPoint{}
	for b := from - from%bs; b <= to; b += bs {
		out = append(out, TSPoint{T: b, Label: time.UnixMilli(b).UTC().Format(layout), Count: acc[b]})
	}
	return out
}

func (o *OLAP) dimMap(dim string) map[dimKey]uint64 {
	switch dim {
	case "country":
		return o.mvGeo
	case "city":
		return o.mvCity
	case "referrer":
		return o.mvRef
	case "device":
		return o.mvDevice
	case "browser":
		return o.mvBrowse
	default:
		return o.mvOS
	}
}

func (o *OLAP) BreakdownMV(codes []uint32, dim string, limit int) ([]DimRow, time.Duration) {
	start := time.Now()
	o.mu.RLock()
	m := o.dimMap(dim)
	acc := map[uint32]uint64{}
	set := map[uint32]bool{}
	for _, c := range codes {
		set[c] = true
	}
	for k, v := range m {
		if set[k.Code] {
			acc[k.Val] += v
		}
	}
	rows := o.rank(acc, limit)
	o.mu.RUnlock()
	return rows, time.Since(start)
}

func (o *OLAP) BreakdownRaw(codes []uint32, dim string, limit int) ([]DimRow, time.Duration) {
	start := time.Now()
	set := map[uint32]bool{}
	for _, c := range codes {
		set[c] = true
	}
	acc := map[uint32]uint64{}
	o.mu.RLock()
	for _, ps := range o.parts {
		for _, p := range ps {
			var col []uint32
			switch dim {
			case "country":
				col = p.Country
			case "city":
				col = p.City
			case "referrer":
				col = p.Ref
			case "device":
				col = p.Device
			case "browser":
				col = p.Browser
			default:
				col = p.OS
			}
			for i := 0; i < len(p.TS); i++ {
				if set[p.Code[i]] {
					acc[col[i]]++
				}
			}
		}
	}
	rows := o.rank(acc, limit)
	o.mu.RUnlock()
	return rows, time.Since(start)
}

func (o *OLAP) rank(acc map[uint32]uint64, limit int) []DimRow {
	var total uint64
	for _, v := range acc {
		total += v
	}
	rows := make([]DimRow, 0, len(acc))
	for k, v := range acc {
		pct := 0.0
		if total > 0 {
			pct = float64(v) / float64(total) * 100
		}
		rows = append(rows, DimRow{Key: o.dict.Val(k), Count: v, Pct: pct})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Count > rows[j].Count })
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
}

func (o *OLAP) Summary(codes []uint32) CodeSummary {
	now := time.Now().UnixMilli()
	s := CodeSummary{}
	o.mu.RLock()
	defer o.mu.RUnlock()
	set := map[uint32]bool{}
	for _, c := range codes {
		set[c] = true
	}
	for k, v := range o.mvHour {
		if !set[k.Code] {
			continue
		}
		s.Total += v
		if k.Bucket >= now-86400000 {
			s.Last24 += v
		}
	}
	for k, v := range o.mvMinute {
		if set[k.Code] && k.Bucket >= now-3600000 {
			s.Last1h += v
		}
	}
	for _, c := range codes {
		s.Bots += o.mvBots[c]
		if f, ok := o.mvFirst[c]; ok && (s.First == 0 || f < s.First) {
			s.First = f
		}
		if l, ok := o.mvLast[c]; ok && l > s.Last {
			s.Last = l
		}
	}
	return s
}

type PartitionInfo struct {
	Day   string `json:"day"`
	Parts int    `json:"parts"`
	Rows  int    `json:"rows"`
}

type OLAPStats struct {
	TotalRows   uint64          `json:"total_rows"`
	Partitions  []PartitionInfo `json:"partitions"`
	PartCount   int             `json:"part_count"`
	Merges      uint64          `json:"merges"`
	Inserts     uint64          `json:"inserts"`
	MVRows      int             `json:"mv_rows"`
	DictEntries int             `json:"dict_entries"`
	MemMB       float64         `json:"approx_mem_mb"`
}

func (o *OLAP) Stats() OLAPStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	st := OLAPStats{TotalRows: o.TotalRows.Load(), Merges: o.Merges.Load(), Inserts: o.Inserts.Load(),
		DictEntries: o.dict.Size()}
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
	st.MVRows = len(o.mvMinute) + len(o.mvHour) + len(o.mvDay) + len(o.mvGeo) + len(o.mvRef) + len(o.mvDevice) + len(o.mvBrowse) + len(o.mvOS)
	st.MemMB = float64(st.TotalRows) * 41 / 1024 / 1024
	return st
}
