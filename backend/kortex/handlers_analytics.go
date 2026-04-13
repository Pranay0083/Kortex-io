package main

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func rangeWindow(rng string) (time.Duration, string) {
	switch rng {
	case "1h":
		return time.Hour, "minute"
	case "6h":
		return 6 * time.Hour, "minute"
	case "24h":
		return 24 * time.Hour, "hour"
	case "7d":
		return 7 * 24 * time.Hour, "hour"
	case "30d":
		return 30 * 24 * time.Hour, "day"
	default:
		return 24 * time.Hour, "hour"
	}
}

func (a *App) userCodes(r *http.Request) ([]uint32, []string) {
	u := currentUser(r)
	links, err := a.store.ListByUser(r.Context(), u.ID, 500)
	if err != nil {
		return nil, nil
	}
	ids := make([]uint32, 0, len(links))
	codes := make([]string, 0, len(links))
	for _, l := range links {
		ids = append(ids, a.olap.CodeID(l.Code))
		codes = append(codes, l.Code)
	}
	return ids, codes
}

func (a *App) codesForRequest(r *http.Request) []uint32 {
	code := chi.URLParam(r, "code")
	if code != "" && code != "all" {
		u := currentUser(r)
		l, err := a.store.GetByCode(r.Context(), code)
		if err != nil || l.UserID != u.ID {
			return nil
		}
		return []uint32{a.olap.CodeID(code)}
	}
	ids, _ := a.userCodes(r)
	return ids
}

func (a *App) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	codes := a.codesForRequest(r)
	if codes == nil {
		writeErr(w, 404, "link not found")
		return
	}
	win, defGran := rangeWindow(r.URL.Query().Get("range"))
	gran := r.URL.Query().Get("gran")
	if gran == "" {
		gran = defGran
	}
	mode := r.URL.Query().Get("mode")
	to := time.Now().UnixMilli()
	from := to - win.Milliseconds()

	var points []TSPoint
	var dur time.Duration
	var scanned uint64
	if mode == "raw" {
		points, dur, scanned = a.olap.TimeSeriesRaw(codes, gran, from, to)
	} else {
		mode = "mv"
		points, dur, scanned = a.olap.TimeSeriesMV(codes, gran, from, to)
	}
	writeJSON(w, 200, map[string]any{
		"points": points, "granularity": gran, "mode": mode,
		"query_us": dur.Microseconds(), "rows_scanned": scanned,
		"from": from, "to": to,
	})
}

func (a *App) handleBreakdown(w http.ResponseWriter, r *http.Request) {
	codes := a.codesForRequest(r)
	if codes == nil {
		writeErr(w, 404, "link not found")
		return
	}
	dim := r.URL.Query().Get("dim")
	if dim == "" {
		dim = "country"
	}
	mode := r.URL.Query().Get("mode")
	limit := qInt(r, "limit", 12)
	var rows []DimRow
	var dur time.Duration
	if mode == "raw" {
		rows, dur = a.olap.BreakdownRaw(codes, dim, limit)
	} else {
		mode = "mv"
		rows, dur = a.olap.BreakdownMV(codes, dim, limit)
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "dim": dim, "mode": mode, "query_us": dur.Microseconds()})
}

func (a *App) handleSummary(w http.ResponseWriter, r *http.Request) {
	codes := a.codesForRequest(r)
	if codes == nil {
		writeErr(w, 404, "link not found")
		return
	}
	s := a.olap.Summary(codes)
	writeJSON(w, 200, s)
}

func (a *App) handleOverview(w http.ResponseWriter, r *http.Request) {
	ids, codes := a.userCodes(r)
	s := a.olap.Summary(ids)
	geo, _ := a.olap.BreakdownMV(ids, "country", 8)
	ref, _ := a.olap.BreakdownMV(ids, "referrer", 8)
	dev, _ := a.olap.BreakdownMV(ids, "device", 5)
	brw, _ := a.olap.BreakdownMV(ids, "browser", 6)
	to := time.Now().UnixMilli()
	series, dur, _ := a.olap.TimeSeriesMV(ids, "hour", to-24*3600*1000, to)
	writeJSON(w, 200, map[string]any{
		"summary": s, "links": len(codes), "geo": geo, "referrer": ref,
		"device": dev, "browser": brw, "series": series, "query_us": dur.Microseconds(),
	})
}

// The headline comparison: pre-aggregated MV vs on-read full scan.
func (a *App) handleQueryRace(w http.ResponseWriter, r *http.Request) {
	codes := a.codesForRequest(r)
	if codes == nil {
		writeErr(w, 404, "link not found")
		return
	}
	win, gran := rangeWindow(r.URL.Query().Get("range"))
	to := time.Now().UnixMilli()
	from := to - win.Milliseconds()

	rawPts, rawDur, rawScan := a.olap.TimeSeriesRaw(codes, gran, from, to)
	mvPts, mvDur, mvScan := a.olap.TimeSeriesMV(codes, gran, from, to)

	rawTotal, mvTotal := uint64(0), uint64(0)
	for _, p := range rawPts {
		rawTotal += p.Count
	}
	for _, p := range mvPts {
		mvTotal += p.Count
	}
	speedup := 0.0
	if mvDur > 0 {
		speedup = float64(rawDur) / float64(mvDur)
	}
	writeJSON(w, 200, map[string]any{
		"granularity": gran,
		"raw":         map[string]any{"us": rawDur.Microseconds(), "rows_scanned": rawScan, "total": rawTotal, "buckets": len(rawPts)},
		"mv":          map[string]any{"us": mvDur.Microseconds(), "rows_scanned": mvScan, "total": mvTotal, "buckets": len(mvPts)},
		"speedup":     speedup,
		"identical":   rawTotal == mvTotal,
		"table_rows":  a.olap.TotalRows.Load(),
	})
}

func (a *App) handleStream(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"events": a.bus.Recent(qInt(r, "limit", 40)),
		"bus":    a.bus.Stats(),
	})
}
