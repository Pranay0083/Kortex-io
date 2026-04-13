package main

import (
	"hash/fnv"
	"sort"
	"strconv"
)

// Consistent hash ring with virtual nodes. Shards are logical Postgres
// tables (links_0..links_N-1) living in one cluster; the ring is what lets
// us move to physical instances later without rewriting the key mapping.
type Ring struct {
	vnodes int
	shards []int
	points []uint32
	owner  map[uint32]int
}

func hash32(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmix32(h.Sum32())
}

// FNV-1a alone leaves short, near-identical keys clustered on the ring.
// The murmur3 finaliser gives the avalanche the ring needs to stay balanced.
func fmix32(h uint32) uint32 {
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return h
}

func NewRing(shardCount, vnodes int) *Ring {
	shards := make([]int, shardCount)
	for i := range shards {
		shards[i] = i
	}
	return buildRing(shards, vnodes)
}

func buildRing(shards []int, vnodes int) *Ring {
	r := &Ring{vnodes: vnodes, shards: shards, owner: map[uint32]int{}}
	for _, s := range shards {
		for v := 0; v < vnodes; v++ {
			p := hash32("shard-" + strconv.Itoa(s) + "#vn" + strconv.Itoa(v))
			r.points = append(r.points, p)
			r.owner[p] = s
		}
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
	return r
}

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

// Compares consistent hashing against naive modulo when resizing the cluster.
func ringReport(oldN, newN, vnodes, samples int, keys []string) RingReport {
	oldRing := NewRing(oldN, vnodes)
	newRing := NewRing(newN, vnodes)
	before := map[string]int{}
	after := map[string]int{}
	moved, naive := 0, 0
	for i := 0; i < samples; i++ {
		var k string
		if i < len(keys) {
			k = keys[i]
		} else {
			k = "key-" + strconv.Itoa(i)
		}
		o := oldRing.Get(k)
		n := newRing.Get(k)
		before[strconv.Itoa(o)]++
		after[strconv.Itoa(n)]++
		if o != n {
			moved++
		}
		h := hash32(k)
		if int(h)%oldN != int(h)%newN {
			naive++
		}
	}
	// Sample the ring for the visualiser (one point per vnode is too dense).
	step := 1
	if vnodes > 24 {
		step = vnodes / 24
	}
	nodes := []RingNode{}
	for idx, p := range newRing.points {
		if idx%step != 0 {
			continue
		}
		nodes = append(nodes, RingNode{
			Shard: newRing.owner[p],
			Angle: float64(p) / float64(^uint32(0)) * 360.0,
			Hash:  p,
		})
	}
	return RingReport{
		Nodes:      nodes,
		Before:     before,
		After:      after,
		MovedPct:   float64(moved) / float64(samples) * 100,
		NaiveMoved: float64(naive) / float64(samples) * 100,
		Samples:    samples,
		VNodes:     vnodes,
	}
}
