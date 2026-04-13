package main

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Kortex epoch: 2025-01-01T00:00:00Z. Buys us ~69 years of 41-bit millisecond space.
const (
	kortexEpoch = int64(1735689600000)
	nodeBits    = uint(10)
	seqBits     = uint(12)
	maxNodeID   = int64(-1) ^ (int64(-1) << nodeBits)
	maxSeq      = int64(-1) ^ (int64(-1) << seqBits)

	// 58-bit scramble space keeps codes at 10 base62 chars while making
	// sequential IDs non-enumerable. Bijective => decode needs no DB lookup.
	scrambleBits = uint(58)
	scrambleMask = uint64(1)<<scrambleBits - 1
	scrambleMult = uint64(0x2545F4914F6CDD1)
)

var scrambleInv = invMod2n(scrambleMult)

// Newton-Raphson modular inverse of an odd number mod 2^64.
func invMod2n(a uint64) uint64 {
	x := a
	for i := 0; i < 6; i++ {
		x = x * (2 - a*x)
	}
	return x
}

const b62alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var b62rev = func() map[byte]uint64 {
	m := make(map[byte]uint64, 62)
	for i := 0; i < len(b62alphabet); i++ {
		m[b62alphabet[i]] = uint64(i)
	}
	return m
}()

func base62Encode(n uint64, pad int) string {
	if n == 0 {
		return strings.Repeat("0", pad)
	}
	buf := make([]byte, 0, 12)
	for n > 0 {
		buf = append(buf, b62alphabet[n%62])
		n /= 62
	}
	for len(buf) < pad {
		buf = append(buf, '0')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

func base62Decode(s string) (uint64, error) {
	var n uint64
	for i := 0; i < len(s); i++ {
		v, ok := b62rev[s[i]]
		if !ok {
			return 0, errors.New("invalid base62 character")
		}
		n = n*62 + v
	}
	return n, nil
}

type Snowflake struct {
	mu       sync.Mutex
	node     int64
	lastMS   int64
	seq      int64
	Issued   atomic.Uint64
	SeqStall atomic.Uint64
	ClockBk  atomic.Uint64
}

func NewSnowflake(node int64) *Snowflake {
	return &Snowflake{node: node & maxNodeID}
}

func (s *Snowflake) Next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	if now < s.lastMS {
		s.ClockBk.Add(1)
		now = s.lastMS
	}
	if now == s.lastMS {
		s.seq = (s.seq + 1) & maxSeq
		if s.seq == 0 {
			s.SeqStall.Add(1)
			for now <= s.lastMS {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.seq = 0
	}
	s.lastMS = now
	s.Issued.Add(1)
	return ((now - kortexEpoch) << (nodeBits + seqBits)) | (s.node << seqBits) | s.seq
}

type SnowflakeParts struct {
	ID        int64  `json:"id,string"`
	Code      string `json:"code"`
	Binary    string `json:"binary"`
	TimeBits  string `json:"time_bits"`
	NodeBits  string `json:"node_bits"`
	SeqBits   string `json:"seq_bits"`
	TimestampMS int64 `json:"timestamp_ms"`
	IssuedAt  string `json:"issued_at"`
	Node      int64  `json:"node"`
	Sequence  int64  `json:"sequence"`
	Scrambled uint64 `json:"scrambled,string"`
}

func encodeCode(id int64) string {
	return base62Encode((uint64(id)*scrambleMult)&scrambleMask, 10)
}

func decodeCode(code string) (int64, error) {
	n, err := base62Decode(code)
	if err != nil {
		return 0, err
	}
	return int64((n * scrambleInv) & scrambleMask), nil
}

func padBits(v int64, width int) string {
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		if v&1 == 1 {
			out[i] = '1'
		} else {
			out[i] = '0'
		}
		v >>= 1
	}
	return string(out)
}

func explodeID(id int64) SnowflakeParts {
	ts := (id >> (nodeBits + seqBits)) + kortexEpoch
	node := (id >> seqBits) & maxNodeID
	seq := id & maxSeq
	tb := padBits(id>>(nodeBits+seqBits), 41)
	nb := padBits(node, int(nodeBits))
	sb := padBits(seq, int(seqBits))
	return SnowflakeParts{
		ID:          id,
		Code:        encodeCode(id),
		Binary:      "0" + tb + nb + sb,
		TimeBits:    tb,
		NodeBits:    nb,
		SeqBits:     sb,
		TimestampMS: ts,
		IssuedAt:    time.UnixMilli(ts).UTC().Format(time.RFC3339Nano),
		Node:        node,
		Sequence:    seq,
		Scrambled:   (uint64(id) * scrambleMult) & scrambleMask,
	}
}
