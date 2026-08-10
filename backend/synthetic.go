package main

// Weighted tables for fake click traffic. Only the boot-time seed and the
// ingest benchmark use them; real redirects are analysed in useragent.go.

// randInt is the one method we need from *rand.Rand, so tests can pass
// their own deterministic source.
type randInt interface{ Intn(int) int }

type countryWeight struct {
	Code   string
	Cities []string
	Weight int
}

var countries = []countryWeight{
	{"IN", []string{"Bengaluru", "Mumbai", "Delhi", "Hyderabad", "Pune"}, 28},
	{"US", []string{"San Francisco", "New York", "Austin", "Seattle", "Chicago"}, 24},
	{"DE", []string{"Berlin", "Munich", "Hamburg"}, 8},
	{"GB", []string{"London", "Manchester"}, 7},
	{"BR", []string{"São Paulo", "Rio de Janeiro"}, 6},
	{"SG", []string{"Singapore"}, 5},
	{"JP", []string{"Tokyo", "Osaka"}, 5},
	{"NG", []string{"Lagos", "Abuja"}, 4},
	{"ID", []string{"Jakarta", "Bandung"}, 4},
	{"FR", []string{"Paris", "Lyon"}, 3},
	{"AU", []string{"Sydney", "Melbourne"}, 3},
	{"CA", []string{"Toronto", "Vancouver"}, 3},
}

type referrerWeight struct {
	Name   string
	Weight int
}

var referrers = []referrerWeight{
	{"direct", 30}, {"x.com", 16}, {"news.ycombinator.com", 13}, {"google.com", 12},
	{"linkedin.com", 9}, {"reddit.com", 7}, {"github.com", 5}, {"whatsapp.com", 4},
	{"t.co", 2}, {"newsletter", 2},
}

type deviceWeight struct {
	Device, Browser, OS string
	Weight              int
}

var devices = []deviceWeight{
	{"mobile", "Chrome Mobile", "Android", 34},
	{"mobile", "Safari Mobile", "iOS", 20},
	{"desktop", "Chrome", "macOS", 15},
	{"desktop", "Chrome", "Windows", 13},
	{"desktop", "Safari", "macOS", 6},
	{"desktop", "Firefox", "Linux", 5},
	{"desktop", "Edge", "Windows", 4},
	{"tablet", "Safari Mobile", "iPadOS", 3},
}

// pickWeighted chooses one item with probability weight/totalWeight: draw a
// number in [0, total) and walk the list subtracting weights until it drops
// below zero.
func pickWeighted[T any](rng randInt, items []T, weight func(T) int) T {
	total := 0
	for _, it := range items {
		total += weight(it)
	}
	n := rng.Intn(total)
	for _, it := range items {
		n -= weight(it)
		if n < 0 {
			return it
		}
	}
	return items[len(items)-1]
}

func pickCountry(rng randInt) (code, city string) {
	c := pickWeighted(rng, countries, func(c countryWeight) int { return c.Weight })
	return c.Code, c.Cities[rng.Intn(len(c.Cities))]
}

func pickReferrer(rng randInt) string {
	return pickWeighted(rng, referrers, func(r referrerWeight) int { return r.Weight }).Name
}

func pickDevice(rng randInt) (device, browser, os string) {
	d := pickWeighted(rng, devices, func(d deviceWeight) int { return d.Weight })
	return d.Device, d.Browser, d.OS
}
