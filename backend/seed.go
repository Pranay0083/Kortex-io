package main

// Boot-time seeding, so a fresh deployment has something to look at:
//   - the admin account (password from ADMIN_PASSWORD)
//   - the shared demo account (DEMO_MODE only; random password, so the only
//     way in is the "Try the demo" button)
//   - 16 admin-owned "bench-NN" links for the load generator
//   - 10 demo links, plus 14 days of synthetic click history with a daily
//     rhythm, a weekend dip and one viral spike
//
// Everything is idempotent: re-running on every boot only fills in what is
// missing.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// --- accounts ------------------------------------------------------------

// seedAdmin makes sure the admin exists and has the configured password.
func (a *App) seedAdmin(ctx context.Context) (*User, error) {
	u, hash, err := a.store.GetUserByEmail(ctx, a.cfg.AdminEmail)
	if err != nil {
		return a.createUser(ctx, a.cfg.AdminEmail, a.cfg.AdminPass, "Kortex Admin", "admin")
	}
	if !verifyPassword(a.cfg.AdminPass, hash) {
		h, err := hashPassword(a.cfg.AdminPass)
		if err != nil {
			return nil, err
		}
		if err := a.store.SetPassword(ctx, u.ID, h); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// seedDemoUser makes sure the shared trial account exists. Its password is
// re-randomised on every boot and never shown, so visitors get in only
// through POST /api/auth/demo, which works only while DEMO_MODE is on.
func (a *App) seedDemoUser(ctx context.Context) (*User, error) {
	pass := randomHex() + randomHex()
	u, _, err := a.store.GetUserByEmail(ctx, a.cfg.DemoEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return a.createUser(ctx, a.cfg.DemoEmail, pass, "Demo visitor", "demo")
	}
	if err != nil {
		return nil, err
	}
	h, err := hashPassword(pass)
	if err != nil {
		return nil, err
	}
	if err := a.store.SetPasswordAndRole(ctx, u.ID, h, "demo"); err != nil {
		return nil, err
	}
	u.Role = "demo"
	return u, nil
}

// --- demo links ----------------------------------------------------------

type seedLink struct {
	URL    string
	Title  string
	Alias  string  // "" = generated code
	Weight float64 // share of the synthetic traffic
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

// seedDemo runs all the seeding steps. Demo links belong to the demo account
// in demo mode (so "Try the demo" never hands out the admin), otherwise to
// the admin.
func (a *App) seedDemo(ctx context.Context) error {
	admin, err := a.seedAdmin(ctx)
	if err != nil {
		return err
	}
	a.adminID = admin.ID
	a.seedBenchLinks(ctx, admin)

	owner, others := admin, []int64{}
	if a.cfg.DemoMode {
		demo, err := a.seedDemoUser(ctx)
		if err != nil {
			return err
		}
		a.demoUserID = demo.ID
		owner = demo
		others = append(others, admin.ID)
	} else if demo, _, err := a.store.GetUserByEmail(ctx, a.cfg.DemoEmail); err == nil {
		others = append(others, demo.ID)
	}

	links, err := a.ensureSeedLinks(ctx, owner, others)
	if err != nil {
		return err
	}
	a.demoCodes = make(map[string]bool, len(links))
	for _, l := range links {
		a.demoCodes[l.Code] = true
		a.bloom.Add(l.Code)
		a.cache.Set(ctx, l)
	}
	if len(links) == 0 || a.olap.TotalRows.Load() > 0 || a.cfg.SeedEvents <= 0 {
		return nil
	}
	a.backfillClicks(links, a.cfg.SeedEvents)

	// The backfill *is* the click history of the seeded links, so their
	// stored totals are reset to match it. That also clears anything a
	// previous run added, keeping the demo numbers the same after every boot.
	totals := make(map[string]int64, len(links))
	for _, l := range links {
		totals[l.Code] = int64(a.olap.Summary([]uint32{a.olap.CodeID(l.Code)}).Total)
	}
	if err := a.store.SetClicks(ctx, totals); err != nil {
		log.Printf("seed: resync click totals: %v", err)
	}
	return nil
}

// ensureSeedLinks returns one link per seedLinks entry, owned by owner. It
// reuses existing ones (moving them from `others` if needed) and recreates
// any that were deleted.
func (a *App) ensureSeedLinks(ctx context.Context, owner *User, others []int64) ([]*Link, error) {
	existing, err := a.store.ListByUser(ctx, owner.ID, 500)
	if err != nil {
		return nil, err
	}
	for _, id := range others {
		more, err := a.store.ListByUser(ctx, id, 500)
		if err != nil {
			return nil, err
		}
		existing = append(existing, more...)
	}

	var links []*Link
	var moved []string
	for i, sl := range seedLinks {
		if l := findSeed(sl, existing); l != nil {
			if l.UserID != owner.ID {
				l.UserID = owner.ID
				moved = append(moved, l.Code)
			}
			links = append(links, l)
			continue
		}
		l := newSeedLink(a.ids.Next(), sl, owner.ID, i)
		if err := a.store.InsertLink(ctx, l); err != nil {
			log.Printf("seed link %q: %v", l.Code, err)
			continue
		}
		links = append(links, l)
	}

	if len(moved) > 0 {
		if err := a.store.ReassignLinks(ctx, moved, owner.ID); err != nil {
			return nil, err
		}
		log.Printf("seed: moved %d demo links to %s", len(moved), owner.Email)
	}
	return links, nil
}

// newSeedLink builds seed link i. Creation dates are spread over the last two
// weeks, and the last link expires in 48h so the UI shows an expiring link.
func newSeedLink(id int64, sl seedLink, ownerID int64, i int) *Link {
	code := encodeCode(id)
	if sl.Alias != "" {
		code = sl.Alias
	}
	l := &Link{
		ID: id, Code: code, LongURL: sl.URL, Title: sl.Title, UserID: ownerID,
		Custom: sl.Alias != "", Redirect: http.StatusFound, Active: true,
		CreatedAt: time.Now().Add(-time.Duration(14-i) * 24 * time.Hour).UTC(),
	}
	if i == len(seedLinks)-1 {
		exp := time.Now().Add(48 * time.Hour).UTC()
		l.ExpiresAt = &exp
	}
	return l
}

// findSeed finds an existing link for a seed entry: by alias if it has one,
// otherwise by destination URL (oldest match wins).
func findSeed(sl seedLink, links []*Link) *Link {
	var best *Link
	for _, l := range links {
		if sl.Alias != "" {
			if l.Code == sl.Alias {
				return l
			}
			continue
		}
		if !l.Custom && l.LongURL == sl.URL && (best == nil || l.CreatedAt.Before(best.CreatedAt)) {
			best = l
		}
	}
	return best
}

// --- synthetic click history ---------------------------------------------

const (
	backfillDays = 14
	// The "viral" window: 8 hours, about two days ago, with ~4x traffic,
	// mostly from Hacker News.
	spikeFrom = 52 * time.Hour
	spikeTo   = 44 * time.Hour
)

// backfillClicks inserts n fake clicks over the last two weeks. Timestamps
// are drawn by rejection sampling: pick a uniform random time, keep it with
// probability proportional to activity(t), else try again. The result
// follows the activity curve.
func (a *App) backfillClicks(links []*Link, n int) {
	rng := rand.New(rand.NewSource(20260601)) // fixed seed: same demo data every boot
	now := time.Now().UTC()
	windowMS := backfillDays * dayMS
	startMS := now.UnixMilli() - windowMS
	spikeStart, spikeEnd := now.Add(-spikeFrom).UnixMilli(), now.Add(-spikeTo).UnixMilli()
	inSpike := func(ts int64) bool { return ts >= spikeStart && ts <= spikeEnd }

	totalWeight := 0.0
	for _, l := range links {
		totalWeight += weightFor(l.Code)
	}

	batch := make([]ClickEvent, 0, ingestBatch)
	for i := 0; i < n; i++ {
		var ts int64
		for {
			ts = startMS + int64(rng.Float64()*float64(windowMS))
			act := activity(time.UnixMilli(ts).UTC(), float64(now.UnixMilli()-ts)/float64(windowMS))
			if inSpike(ts) {
				act *= 4.2
			}
			if rng.Float64() < act/1.9 { // 1.9 is just above the curve's peak
				break
			}
		}
		l := pickLinkByWeight(links, totalWeight, rng)
		country, city := pickCountry(rng)
		dev, brw, os := pickDevice(rng)
		ref := pickReferrer(rng)
		if inSpike(ts) && rng.Float64() < 0.6 {
			ref = "news.ycombinator.com"
		}
		batch = append(batch, ClickEvent{
			TS: ts, Code: l.Code, UserID: l.UserID, Country: country, City: city, Ref: ref,
			Device: dev, Browser: brw, OS: os, Bot: rng.Intn(1000) < 38, Status: http.StatusFound, Hit: true,
		})
		if len(batch) == cap(batch) {
			a.olap.Insert(batch)
			batch = batch[:0]
		}
	}
	a.olap.Insert(batch)
}

// activity is the relative traffic level at time t. age is 0 for now and
// 1 for two weeks ago.
//   - two daily peaks (Gaussian bumps at 10:00 and 20:00 UTC)
//   - weekends at 62%
//   - traffic grows over the fortnight (older = quieter)
func activity(t time.Time, age float64) float64 {
	hour := float64(t.Hour()) + float64(t.Minute())/60
	act := 0.35 + 0.45*math.Exp(-math.Pow(hour-10, 2)/12) + 0.55*math.Exp(-math.Pow(hour-20, 2)/9)
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		act *= 0.62
	}
	return act * (0.45 + 0.55*(1-age))
}

func weightFor(code string) float64 {
	for _, sl := range seedLinks {
		if sl.Alias == code {
			return sl.Weight
		}
	}
	return 0.4 // generated-code links
}

func pickLinkByWeight(links []*Link, total float64, rng *rand.Rand) *Link {
	n := rng.Float64() * total
	for _, l := range links {
		n -= weightFor(l.Code)
		if n <= 0 {
			return l
		}
	}
	return links[0]
}

// --- benchmark links -----------------------------------------------------

const benchLinkCount = 16

// seedBenchLinks makes sure the admin owns "bench-01".."bench-16" (a reserved
// alias prefix, so users can't take them), recreating any that were deleted.
func (a *App) seedBenchLinks(ctx context.Context, admin *User) {
	a.benchLinks = a.benchLinks[:0]
	for i := 1; i <= benchLinkCount; i++ {
		code := fmt.Sprintf("%s%02d", benchAliasPrefix, i)
		l, err := a.store.GetByCode(ctx, code)
		switch {
		case err == nil && l.UserID != admin.ID:
			log.Printf("bench link %q is owned by another account; skipping", code)
			continue
		case errors.Is(err, pgx.ErrNoRows):
			l = &Link{
				ID: a.ids.Next(), Code: code, LongURL: "https://example.com/",
				Title: fmt.Sprintf("Benchmark target %02d", i), UserID: admin.ID,
				Custom: true, Redirect: http.StatusFound, Active: true, CreatedAt: time.Now().UTC(),
			}
			if err := a.store.InsertLink(ctx, l); err != nil {
				log.Printf("bench link %q: %v", code, err)
				continue
			}
		case err != nil:
			log.Printf("bench link %q: %v", code, err)
			continue
		}
		a.bloom.Add(l.Code)
		a.cache.Set(ctx, l)
		a.benchLinks = append(a.benchLinks, l.Code)
	}
}
