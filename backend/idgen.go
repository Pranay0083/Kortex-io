package main

// ID generation and short codes.
//
// Every link and user gets a 64-bit Snowflake ID:
//
//	| 1 bit unused | 41 bits ms since kortexEpoch | 10 bits node | 12 bits sequence |
//
// IDs are unique across nodes without coordination, and they sort by time.
//
// A short code is made from the ID in two steps:
//  1. scramble: multiply by a large odd constant (mod 2^64). This is a
//     bijection, so it can be undone, but consecutive IDs become very
//     different numbers and nobody can guess the next link.
//  2. base62-encode the result, padded to 11 characters (62^11 > 2^64).
//
// So decoding a code back to its ID needs no database lookup.

import (
	"errors"
	"math/bits"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	kortexEpoch = int64(1735689600000) // 2025-01-01T00:00:00Z; 41 bits of ms lasts until ~2094
	timeBits    = 41
	nodeBits    = 10
	seqBits     = 12
	maxNodeID   = int64(1)<<nodeBits - 1 // 1023
	maxSeq      = int64(1)<<seqBits - 1  // 4095 IDs per node per millisecond

	scrambleMult = uint64(0x2545F4914F6CDD1) // any odd number works; this one mixes bits well
	codeLen      = 11
)

// scrambleInv undoes scrambleMult: id * mult * inv == id (mod 2^64).
var scrambleInv = invMod2n(scrambleMult)

// invMod2n finds the inverse of an odd number mod 2^64 with Newton's method.
// Each step doubles the number of correct low bits (3 -> 6 -> ... -> 96),
// so 6 steps are enough for 64 bits.
func invMod2n(a uint64) uint64 {
	x := a
	for i := 0; i < 6; i++ {
		x = x * (2 - a*x)
	}
	return x
}

func scramble(id int64) uint64 { return uint64(id) * scrambleMult }

func encodeCode(id int64) string { return base62Encode(scramble(id), codeLen) }

func decodeCode(code string) (int64, error) {
	n, err := base62Decode(code)
	if err != nil {
		return 0, err
	}
	id := int64(n * scrambleInv)
	if id < 0 {
		return 0, errors.New("not a kortex code") // real IDs never set the sign bit
	}
	return id, nil
}

// --- base62 --------------------------------------------------------------

const b62alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// b62value[c] is the digit value of character c, or -1 if c is not base62.
var b62value = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(b62alphabet); i++ {
		t[b62alphabet[i]] = int8(i)
	}
	return t
}()

// base62Encode writes n in base62, left-padded with '0' to at least pad chars.
func base62Encode(n uint64, pad int) string {
	var buf [codeLen]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = b62alphabet[n%62]
		n /= 62
	}
	digits := string(buf[i:])
	if len(digits) < pad {
		digits = strings.Repeat("0", pad-len(digits)) + digits
	}
	return digits
}

var errB62Overflow = errors.New("base62 value overflows 64 bits")

// base62Decode parses s, rejecting anything that would not fit in a uint64.
func base62Decode(s string) (uint64, error) {
	if s == "" {
		return 0, errors.New("empty base62 string")
	}
	if len(s) > codeLen {
		return 0, errB62Overflow
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		v := b62value[s[i]]
		if v < 0 {
			return 0, errors.New("invalid base62 character")
		}
		// n = n*62 + v, checking for overflow at each step
		hi, lo := bits.Mul64(n, 62)
		lo, carry := bits.Add64(lo, uint64(v), 0)
		if hi != 0 || carry != 0 {
			return 0, errB62Overflow
		}
		n = lo
	}
	return n, nil
}

// --- Snowflake generator -------------------------------------------------

type Snowflake struct {
	mu     sync.Mutex
	node   int64
	lastMS int64
	seq    int64

	// counters for the ops dashboard
	Issued         atomic.Uint64
	SeqExhausted   atomic.Uint64 // times we used all 4096 IDs in one ms and had to wait
	ClockBackwards atomic.Uint64 // times the wall clock went backwards
}

func NewSnowflake(node int64) *Snowflake {
	return &Snowflake{node: node & maxNodeID}
}

// Next returns a new unique ID. It is safe for concurrent use.
func (s *Snowflake) Next() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	if now < s.lastMS {
		// Clock went backwards (e.g. NTP adjustment). Keep using the last
		// timestamp so IDs never repeat or go backwards.
		s.ClockBackwards.Add(1)
		now = s.lastMS
	}
	if now == s.lastMS {
		s.seq = (s.seq + 1) & maxSeq
		if s.seq == 0 {
			// All sequence numbers for this ms are used: wait for the next ms.
			s.SeqExhausted.Add(1)
			for now <= s.lastMS {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		s.seq = 0
	}
	s.lastMS = now
	s.Issued.Add(1)
	return (now-kortexEpoch)<<(nodeBits+seqBits) | s.node<<seqBits | s.seq
}

// SnowflakeParts is an ID split into its fields, for the lab UI.
type SnowflakeParts struct {
	ID          int64  `json:"id,string"`
	Code        string `json:"code"`
	Binary      string `json:"binary"`
	TimeBits    string `json:"time_bits"`
	NodeBits    string `json:"node_bits"`
	SeqBits     string `json:"seq_bits"`
	TimestampMS int64  `json:"timestamp_ms"`
	IssuedAt    string `json:"issued_at"`
	Node        int64  `json:"node"`
	Sequence    int64  `json:"sequence"`
	Scrambled   uint64 `json:"scrambled,string"`
}

func explodeID(id int64) SnowflakeParts {
	sinceEpoch := id >> (nodeBits + seqBits)
	node := (id >> seqBits) & maxNodeID
	seq := id & maxSeq
	ts := sinceEpoch + kortexEpoch

	tb := binaryString(sinceEpoch, timeBits)
	nb := binaryString(node, nodeBits)
	sb := binaryString(seq, seqBits)
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
		Scrambled:   scramble(id),
	}
}

// binaryString formats the low `width` bits of v as 0s and 1s.
func binaryString(v int64, width int) string {
	out := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		out[i] = '0' + byte(v&1)
		v >>= 1
	}
	return string(out)
}
