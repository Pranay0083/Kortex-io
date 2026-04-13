package main

import (
	"context"
	"math"
	"math/rand"
	"time"
)

type seedLink struct {
	URL    string
	Title  string
	Alias  string
	Weight float64
}

var seedLinks = []seedLink{
	{"https://clickhouse.com/docs/en/engines/table-engines/mergetree-family/mergetree", "MergeTree engine docs", "mergetree", 1.0},
	{"https://kafka.apache.org/documentation/#design", "Kafka design doc", "kafka-design", 0.72},
	{"https://redis.io/docs/latest/develop/reference/eviction/", "Redis eviction policies", "", 0.55},
	{"https://en.wikipedia.org/wiki/Consistent_hashing", "Consistent hashing", "hashring", 0.44},
	{"https://github.com/twitter-archive/snowflake", "Twitter Snowflake", "snowflake", 0.38},
	{"https://www.postgresql.org/docs/current/ddl-partitioning.html", "Postgres partitioning", "", 0.30},
	{"https://grafana.com/docs/k6/latest/", "k6 load testing", "k6", 0.22},
	{"https://nginx.org/en/docs/http/ngx_http_upstream_module.html", "nginx upstream module", "", 0.16},
	{"https://go.dev/blog/pipelines", "Go concurrency pipelines", "", 0.11},
	{"https://sre.google/sre-book/service-level-objectives/", "Google SRE: SLOs", "slo", 0.08},
}

// Backfills 14 days of click history with a realistic diurnal + weekly shape
// plus one viral spike, so the dashboard is meaningful on first load.
func (a *App) seedDemo(ctx context.Context) error {
	admin, err := a.seedAdmin(ctx)
	if err != nil {
		return err
	}
	existing, err := a.store.ListByUser(ctx, admin.ID, 50)
	if err != nil {
		return err
	}
	created := []*Link{}
	if len(existing) == 0 {
		for i, sl := range seedLinks {
			id := a.sf.Next()
			code := encodeCode(id)
			if sl.Alias != "" {
				code = sl.Alias
			}
			l := &Link{
				ID: id, Code: code, LongURL: sl.URL, Title: sl.Title, UserID: admin.ID,
				Custom: sl.Alias != "", Redirect: 302, Active: true,
				CreatedAt: time.Now().Add(-time.Duration(14-i) * 24 * time.Hour).UTC(),
			}
			if i == len(seedLinks)-1 {
				exp := time.Now().Add(48 * time.Hour).UTC()
				l.ExpiresAt = &exp
			}
			if err := a.store.InsertLink(ctx, l); err != nil {
				return err
			}
			created = append(created, l)
		}
	} else {
		created = existing
	}

	for _, l := range created {
		a.bloom.Add(l.Code)
		a.olap.CodeID(l.Code)
		a.cache.Set(ctx, l)
	}

	if a.olap.TotalRows.Load() > 0 || a.cfg.SeedEvents <= 0 {
		return nil
	}

	rng := rand.New(rand.NewSource(20260601))
	now := time.Now().UTC()
	windowMS := int64(14 * 24 * 3600 * 1000)
	startMS := now.UnixMilli() - windowMS
	spikeStart := now.Add(-52 * time.Hour).UnixMilli()
	spikeEnd := now.Add(-44 * time.Hour).UnixMilli()

	totalW := 0.0
	for _, l := range created {
		totalW += weightFor(l.Code)
	}

	batch := make([]ClickEvent, 0, 20000)
	target := a.cfg.SeedEvents
	for i := 0; i < target; i++ {
		// diurnal shape: pick a timestamp, reject-sample against activity curve
		var ts int64
		for {
			ts = startMS + int64(rng.Float64()*float64(windowMS))
			t := time.UnixMilli(ts).UTC()
			hour := float64(t.Hour()) + float64(t.Minute())/60
			// two humps: 10:00 and 20:00 UTC
			act := 0.35 + 0.45*math.Exp(-math.Pow(hour-10, 2)/12) + 0.55*math.Exp(-math.Pow(hour-20, 2)/9)
			if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
				act *= 0.62
			}
			// recency ramp: traffic grows over the fortnight
			age := float64(now.UnixMilli()-ts) / float64(windowMS)
			act *= 0.45 + 0.55*(1-age)
			if ts >= spikeStart && ts <= spikeEnd {
				act *= 4.2
			}
			if rng.Float64() < act/1.9 {
				break
			}
		}
		l := pickWeighted(created, totalW, rng)
		country, city := pickCountry(rng)
		dev, brw, os := pickDevice(rng)
		ref := pickReferrer(rng)
		if ts >= spikeStart && ts <= spikeEnd && rng.Float64() < 0.6 {
			ref = "news.ycombinator.com"
		}
		batch = append(batch, ClickEvent{
			TS: ts, Code: l.Code, UserID: l.UserID, Country: country, City: city, Ref: ref,
			Device: dev, Browser: brw, OS: os, Bot: rng.Intn(1000) < 38, Status: 302, Hit: true,
		})
		if len(batch) == cap(batch) {
			a.olap.Insert(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		a.olap.Insert(batch)
	}

	// reflect the backfill in the source of truth (only for freshly created links)
	if len(existing) == 0 {
		for _, l := range created {
			s := a.olap.Summary([]uint32{a.olap.CodeID(l.Code)})
			a.store.BumpClicks(l.Code, int64(s.Total))
		}
		a.store.flushClicks(ctx)
	}
	return nil
}

func weightFor(code string) float64 {
	for _, sl := range seedLinks {
		if sl.Alias == code {
			return sl.Weight
		}
	}
	return 0.4
}

func pickWeighted(links []*Link, total float64, rng *rand.Rand) *Link {
	n := rng.Float64() * total
	for _, l := range links {
		n -= weightFor(l.Code)
		if n <= 0 {
			return l
		}
	}
	return links[0]
}
