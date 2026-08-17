package main

// /api/analytics/* handlers. All are read-only queries against the OLAP
// store (olap.go), limited to links the caller owns. ?mode=raw switches from
// the pre-aggregated views to a full scan, so the two can be compared.

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// rangeOption is a ?range= value: how far back to look and the default
// chart bucket size for that span.
type rangeOption struct {
	span time.Duration
	gran string
}

var ranges = map[string]rangeOption{
	"1h":  {time.Hour, "minute"},
	"6h":  {6 * time.Hour, "minute"},
	"24h": {24 * time.Hour, "hour"},
	"7d":  {7 * 24 * time.Hour, "hour"},
	"30d": {30 * 24 * time.Hour, "day"},
}

const defaultRange = "24h"

// window is the time range a request asks about, in unix ms.
type window struct {
	Range string
	Gran  string
	From  int64
	To    int64
}

func requestWindow(r *http.Request) window {
	name := r.URL.Query().Get("range")
	opt, ok := ranges[name]
	if !ok {
		name, opt = defaultRange, ranges[defaultRange]
	}
	to := time.Now().UnixMilli()
	return window{Range: name, Gran: opt.gran, From: to - opt.span.Milliseconds(), To: to}
}

// userCodes returns the OLAP ids and codes of the caller's links (newest 500).
func (a *App) userCodes(r *http.Request) ([]uint32, []string, error) {
	links, err := a.store.ListByUser(r.Context(), currentUser(r).ID, 500)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]uint32, len(links))
	codes := make([]string, len(links))
	for i, l := range links {
		ids[i] = a.olap.CodeID(l.Code)
		codes[i] = l.Code
	}
	return ids, codes, nil
}

// codesForRequest turns the {code} path param into OLAP ids: one owned code,
// or all of the caller's links for "all" / empty.
func (a *App) codesForRequest(r *http.Request) ([]uint32, error) {
	code := chi.URLParam(r, "code")
	if code != "" && code != "all" {
		if _, err := a.ownedLink(r, code); err != nil {
			return nil, err
		}
		return []uint32{a.olap.CodeID(code)}, nil
	}
	ids, _, err := a.userCodes(r)
	return ids, err
}

func sumCounts(points []TSPoint) uint64 {
	var n uint64
	for _, p := range points {
		n += p.Count
	}
	return n
}

func (a *App) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	codes, err := a.codesForRequest(r)
	if err != nil {
		a.fail(w, "analytics codes", err)
		return
	}
	win := requestWindow(r)
	gran := r.URL.Query().Get("gran")
	if gran == "" {
		gran = win.Gran
	}
	if !validGran(gran) {
		writeErr(w, http.StatusBadRequest, "gran must be minute, hour or day")
		return
	}
	// Minute buckets only exist for short windows (and would be too many).
	if gran == "minute" && win.To-win.From > dayMS {
		gran = "hour"
	}

	query, mode := a.olap.TimeSeriesMV, "mv"
	if r.URL.Query().Get("mode") == "raw" {
		query, mode = a.olap.TimeSeriesRaw, "raw"
	}
	points, dur, scanned := query(codes, gran, win.From, win.To)
	writeJSON(w, http.StatusOK, map[string]any{
		"points": points, "granularity": gran, "mode": mode,
		"query_us": dur.Microseconds(), "rows_scanned": scanned,
		"from": win.From, "to": win.To,
	})
}

func (a *App) handleBreakdown(w http.ResponseWriter, r *http.Request) {
	dim := r.URL.Query().Get("dim")
	if dim == "" {
		dim = "country"
	}
	if _, ok := dimIndex[dim]; !ok {
		writeErr(w, http.StatusBadRequest, "dim must be one of country, city, referrer, device, browser, os")
		return
	}
	codes, err := a.codesForRequest(r)
	if err != nil {
		a.fail(w, "analytics codes", err)
		return
	}
	win := requestWindow(r)

	query, mode := a.olap.BreakdownMV, "mv"
	if r.URL.Query().Get("mode") == "raw" {
		query, mode = a.olap.BreakdownRaw, "raw"
	}
	rows, dur := query(codes, dim, win.From, win.To, qLimit(r, 12))
	writeJSON(w, http.StatusOK, map[string]any{
		"rows": rows, "dim": dim, "mode": mode, "range": win.Range,
		"from": win.From, "to": win.To, "query_us": dur.Microseconds(),
	})
}

func (a *App) handleSummary(w http.ResponseWriter, r *http.Request) {
	codes, err := a.codesForRequest(r)
	if err != nil {
		a.fail(w, "analytics codes", err)
		return
	}
	writeJSON(w, http.StatusOK, a.olap.Summary(codes))
}

// handleOverview is the dashboard's single call: totals, top breakdowns and
// the click series for all of the caller's links.
func (a *App) handleOverview(w http.ResponseWriter, r *http.Request) {
	ids, codes, err := a.userCodes(r)
	if err != nil {
		a.internalErr(w, "list links", err)
		return
	}
	win := requestWindow(r)
	top := func(dim string, n int) []DimRow {
		rows, _ := a.olap.BreakdownMV(ids, dim, win.From, win.To, n)
		return rows
	}
	s := a.olap.Summary(ids)
	series, dur, _ := a.olap.TimeSeriesMV(ids, win.Gran, win.From, win.To)
	s.Window = sumCounts(series)
	writeJSON(w, http.StatusOK, map[string]any{
		"summary": s, "links": len(codes),
		"geo": top("country", 8), "referrer": top("referrer", 8), "device": top("device", 5), "browser": top("browser", 6),
		"series": series, "query_us": dur.Microseconds(),
		"range": win.Range, "granularity": win.Gran, "from": win.From, "to": win.To,
	})
}

// handleQueryRace runs the same time-series query both ways (pre-aggregated
// MV vs full scan) and reports the speed-up and whether the totals match.
func (a *App) handleQueryRace(w http.ResponseWriter, r *http.Request) {
	codes, err := a.codesForRequest(r)
	if err != nil {
		a.fail(w, "analytics codes", err)
		return
	}
	win := requestWindow(r)
	rawPts, rawDur, rawScan := a.olap.TimeSeriesRaw(codes, win.Gran, win.From, win.To)
	mvPts, mvDur, mvScan := a.olap.TimeSeriesMV(codes, win.Gran, win.From, win.To)
	rawTotal, mvTotal := sumCounts(rawPts), sumCounts(mvPts)

	speedup := 0.0
	if mvDur > 0 {
		speedup = float64(rawDur) / float64(mvDur)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"granularity": win.Gran,
		"raw":         map[string]any{"us": rawDur.Microseconds(), "rows_scanned": rawScan, "total": rawTotal, "buckets": len(rawPts)},
		"mv":          map[string]any{"us": mvDur.Microseconds(), "rows_scanned": mvScan, "total": mvTotal, "buckets": len(mvPts)},
		"speedup":     speedup,
		"identical":   rawTotal == mvTotal,
		"table_rows":  a.olap.TotalRows.Load(),
	})
}

// handleStream shows the latest events on the click bus, only for the
// caller's links (admins see everything). Client IPs are never stored on
// events.
func (a *App) handleStream(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var keep func(*ClickEvent) bool
	if u.Role != "admin" {
		keep = func(e *ClickEvent) bool { return e.UserID == u.ID }
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": a.bus.Recent(clamp(qInt(r, "limit", 40), 1, recentSize), keep),
		"bus":    a.bus.Stats(),
	})
}
