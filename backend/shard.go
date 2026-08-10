package main

// Consistent hash ring: decides which Postgres shard (links_s0..links_sN-1)
// stores a short code.
//
// Each shard is placed on a 32-bit ring at `vnodes` pseudo-random points. A
// key is hashed onto the ring and belongs to the first point clockwise from
// it. When a shard is added, only keys that land just before its new points
// move, about 1/N of them, while `hash % N` would move almost every key. Many
// virtual nodes per shard keep the load even.

import (
	"hash/fnv"
	"sort"
	"strconv"
)

type Ring struct {
	vnodes int
	shards []int
	points []uint32       // sorted ring positions
	owner  map[uint32]int // ring position -> shard
}

// hash32 is FNV-1a followed by the murmur3 finaliser. FNV alone leaves short,
// similar keys ("shard-1#vn1", "shard-1#vn2") bunched together on the ring;
// the finaliser spreads them out.
func hash32(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmix32(h.Sum32())
}

func fmix32(h uint32) uint32 {
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return h
}

func NewRing(shardCount, vnodes int) *Ring {
	r := &Ring{vnodes: vnodes, owner: map[uint32]int{}}
	for s := 0; s < shardCount; s++ {
		r.shards = append(r.shards, s)
		for v := 0; v < vnodes; v++ {
			p := hash32("shard-" + strconv.Itoa(s) + "#vn" + strconv.Itoa(v))
			r.points = append(r.points, p)
			r.owner[p] = s
		}
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
	return r
}

// Get returns the shard that owns key: binary-search for the first ring
// point >= hash(key), wrapping around to the start.
func (r *Ring) Get(key string) int {
	if len(r.points) == 0 {
		return 0
	}
	h := hash32(key)
	i := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	if i == len(r.points) {
		i = 0
	}
	return r.owner[r.points[i]]
}

func (r *Ring) ShardCount() int { return len(r.shards) }

// --- lab: resize simulation ---------------------------------------------

type RingNode struct {
	Shard int     `json:"shard"`
	Angle float64 `json:"angle"`
	Hash  uint32  `json:"hash"`
}

type RingReport struct {
	Nodes      []RingNode     `json:"nodes"`
	Before     map[string]int `json:"before"`
	After      map[string]int `json:"after"`
	MovedPct   float64        `json:"moved_pct"`
	NaiveMoved float64        `json:"naive_moved_pct"`
	Samples    int            `json:"samples"`
	VNodes     int            `json:"vnodes"`
}

// ringReport resizes the cluster from oldN to newN shards and measures how
// many sample keys change shard with the ring vs. with naive `hash % N`.
func ringReport(oldN, newN, vnodes, samples int, keys []string) RingReport {
	oldRing := NewRing(oldN, vnodes)
	newRing := NewRing(newN, vnodes)
	before := map[string]int{}
	after := map[string]int{}
	moved, naiveMoved := 0, 0
	for i := 0; i < samples; i++ {
		k := "key-" + strconv.Itoa(i)
		if i < len(keys) {
			k = keys[i]
		}
		o, n := oldRing.Get(k), newRing.Get(k)
		before[strconv.Itoa(o)]++
		after[strconv.Itoa(n)]++
		if o != n {
			moved++
		}
		h := int(hash32(k))
		if h%oldN != h%newN {
			naiveMoved++
		}
	}
	return RingReport{
		Nodes:      ringPreview(newRing, 24),
		Before:     before,
		After:      after,
		MovedPct:   float64(moved) / float64(samples) * 100,
		NaiveMoved: float64(naiveMoved) / float64(samples) * 100,
		Samples:    samples,
		VNodes:     vnodes,
	}
}

// ringPreview samples about perShard points per shard for the visualiser;
// drawing every vnode would be too dense.
func ringPreview(r *Ring, perShard int) []RingNode {
	step := max(r.vnodes/perShard, 1)
	nodes := []RingNode{}
	for i, p := range r.points {
		if i%step != 0 {
			continue
		}
		nodes = append(nodes, RingNode{
			Shard: r.owner[p],
			Angle: float64(p) / float64(^uint32(0)) * 360.0,
			Hash:  p,
		})
	}
	return nodes
}
