package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Link struct {
	ID        int64      `json:"id,string"`
	Code      string     `json:"code"`
	LongURL   string     `json:"long_url"`
	Title     string     `json:"title"`
	UserID    int64      `json:"user_id,string"`
	Custom    bool       `json:"custom"`
	Redirect  int        `json:"redirect_type"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	Clicks    int64      `json:"clicks"`
	Active    bool       `json:"active"`
	Shard     int        `json:"shard"`
}

type User struct {
	ID        int64     `json:"id,string"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	pool  *pgxpool.Pool
	ring  *Ring
	pendMu sync.Mutex
	pend   map[string]int64
	Reads  uint64
	Writes uint64
}

func shardTable(s int) string { return "links_s" + strconv.Itoa(s) }

func NewStore(ctx context.Context, url string, ring *Ring) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 24
	cfg.MinConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	s := &Store{pool: pool, ring: ring, pend: map[string]int64{}}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id BIGINT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'user',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}
	for i := 0; i < s.ring.ShardCount(); i++ {
		t := shardTable(i)
		stmts = append(stmts,
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				id BIGINT PRIMARY KEY,
				code TEXT UNIQUE NOT NULL,
				long_url TEXT NOT NULL,
				title TEXT NOT NULL DEFAULT '',
				user_id BIGINT NOT NULL,
				custom BOOLEAN NOT NULL DEFAULT false,
				redirect_type INT NOT NULL DEFAULT 302,
				expires_at TIMESTAMPTZ,
				created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
				clicks BIGINT NOT NULL DEFAULT 0,
				active BOOLEAN NOT NULL DEFAULT true
			)`, t),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_user_idx ON %s (user_id, created_at DESC)`, t, t),
		)
	}
	for _, q := range stmts {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *Store) ShardFor(code string) int { return s.ring.Get(code) }

func scanLink(row pgx.Row, shard int) (*Link, error) {
	var l Link
	err := row.Scan(&l.ID, &l.Code, &l.LongURL, &l.Title, &l.UserID, &l.Custom, &l.Redirect,
		&l.ExpiresAt, &l.CreatedAt, &l.Clicks, &l.Active)
	if err != nil {
		return nil, err
	}
	l.Shard = shard
	return &l, nil
}

const linkCols = `id, code, long_url, title, user_id, custom, redirect_type, expires_at, created_at, clicks, active`

func (s *Store) InsertLink(ctx context.Context, l *Link) error {
	shard := s.ShardFor(l.Code)
	l.Shard = shard
	q := fmt.Sprintf(`INSERT INTO %s (id, code, long_url, title, user_id, custom, redirect_type, expires_at, created_at, clicks, active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, shardTable(shard))
	_, err := s.pool.Exec(ctx, q, l.ID, l.Code, l.LongURL, l.Title, l.UserID, l.Custom, l.Redirect,
		l.ExpiresAt, l.CreatedAt, l.Clicks, l.Active)
	return err
}

func (s *Store) GetByCode(ctx context.Context, code string) (*Link, error) {
	shard := s.ShardFor(code)
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE code=$1`, linkCols, shardTable(shard))
	return scanLink(s.pool.QueryRow(ctx, q, code), shard)
}

func (s *Store) CodeExists(ctx context.Context, code string) (bool, error) {
	shard := s.ShardFor(code)
	q := fmt.Sprintf(`SELECT 1 FROM %s WHERE code=$1`, shardTable(shard))
	var n int
	err := s.pool.QueryRow(ctx, q, code).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListByUser(ctx context.Context, userID int64, limit int) ([]*Link, error) {
	out := []*Link{}
	for i := 0; i < s.ring.ShardCount(); i++ {
		q := fmt.Sprintf(`SELECT %s FROM %s WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, linkCols, shardTable(i))
		rows, err := s.pool.Query(ctx, q, userID, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			l, err := scanLink(rows, i)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, l)
		}
		rows.Close()
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) AllCodes(ctx context.Context) ([]*Link, error) {
	out := []*Link{}
	for i := 0; i < s.ring.ShardCount(); i++ {
		q := fmt.Sprintf(`SELECT %s FROM %s ORDER BY clicks DESC`, linkCols, shardTable(i))
		rows, err := s.pool.Query(ctx, q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			l, err := scanLink(rows, i)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, l)
		}
		rows.Close()
	}
	return out, nil
}

func (s *Store) DeleteLink(ctx context.Context, code string, userID int64) (bool, error) {
	shard := s.ShardFor(code)
	q := fmt.Sprintf(`DELETE FROM %s WHERE code=$1 AND user_id=$2`, shardTable(shard))
	tag, err := s.pool.Exec(ctx, q, code, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) SetActive(ctx context.Context, code string, userID int64, active bool) error {
	shard := s.ShardFor(code)
	q := fmt.Sprintf(`UPDATE %s SET active=$3 WHERE code=$1 AND user_id=$2`, shardTable(shard))
	_, err := s.pool.Exec(ctx, q, code, userID, active)
	return err
}

// Click counters are buffered in memory and flushed in batches; one UPDATE
// per redirect would cap us at Postgres write throughput.
func (s *Store) BumpClicks(code string, n int64) {
	s.pendMu.Lock()
	s.pend[code] += n
	s.pendMu.Unlock()
}

func (s *Store) StartClickFlusher(ctx context.Context) {
	go func() {
		t := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.flushClicks(ctx)
			}
		}
	}()
}

func (s *Store) flushClicks(ctx context.Context) {
	s.pendMu.Lock()
	if len(s.pend) == 0 {
		s.pendMu.Unlock()
		return
	}
	pend := s.pend
	s.pend = map[string]int64{}
	s.pendMu.Unlock()

	byShard := map[int][]string{}
	amounts := map[string]int64{}
	for code, n := range pend {
		sh := s.ShardFor(code)
		byShard[sh] = append(byShard[sh], code)
		amounts[code] = n
	}
	for sh, codes := range byShard {
		var sb strings.Builder
		args := []any{}
		sb.WriteString(fmt.Sprintf("UPDATE %s AS l SET clicks = l.clicks + v.n FROM (VALUES ", shardTable(sh)))
		for i, c := range codes {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(fmt.Sprintf("($%d::text,$%d::bigint)", len(args)+1, len(args)+2))
			args = append(args, c, amounts[c])
		}
		sb.WriteString(") AS v(code,n) WHERE l.code = v.code")
		_, _ = s.pool.Exec(ctx, sb.String(), args...)
	}
}

func (s *Store) PoolStats() map[string]any {
	st := s.pool.Stat()
	return map[string]any{
		"total_conns":    st.TotalConns(),
		"idle_conns":     st.IdleConns(),
		"acquired_conns": st.AcquiredConns(),
		"max_conns":      st.MaxConns(),
		"acquire_count":  st.AcquireCount(),
	}
}

func (s *Store) ShardDistribution(ctx context.Context) []map[string]any {
	out := []map[string]any{}
	for i := 0; i < s.ring.ShardCount(); i++ {
		var n int64
		var clicks int64
		_ = s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*), COALESCE(sum(clicks),0) FROM %s`, shardTable(i))).Scan(&n, &clicks)
		out = append(out, map[string]any{"shard": i, "table": shardTable(i), "links": n, "clicks": clicks})
	}
	return out
}
