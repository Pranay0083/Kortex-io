package main

// PostgreSQL storage. All SQL lives in this file.
//
//   - users: one ordinary table.
//   - links: split across N tables, links_s0..links_sN-1. The consistent
//     hash ring (shard.go) picks the table from the short code, so a lookup
//     by code always hits exactly one shard. Listing a user's links fans out
//     to every shard and merges the results.
//   - click counters: redirects don't UPDATE Postgres. They bump an
//     in-memory counter that a background flusher writes in one batched
//     UPDATE per shard every 2 seconds.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	pool *pgxpool.Pool
	ring *Ring

	pendingMu     sync.Mutex
	pendingClicks map[string]int64 // code -> clicks not yet written

	shardStatsMu sync.Mutex
	shardStatsAt time.Time
	shardStats   []map[string]any
}

const (
	clickFlushInterval = 2 * time.Second
	flushChunk         = 1000 // codes per batched UPDATE
)

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
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
	s := &Store{pool: pool, ring: ring, pendingClicks: map[string]int64{}}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// migrate creates the users table and one links table (plus index) per shard.
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

// ShardFor returns the shard that stores code.
func (s *Store) ShardFor(code string) int { return s.ring.Get(code) }

// groupByShard buckets codes by the shard that owns them.
func (s *Store) groupByShard(codes []string) map[int][]string {
	out := map[int][]string{}
	for _, c := range codes {
		sh := s.ShardFor(c)
		out[sh] = append(out[sh], c)
	}
	return out
}

// --- users ---------------------------------------------------------------

func normEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `SELECT id, email, name, role, created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByEmail also returns the bcrypt hash, for login.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, string, error) {
	var u User
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT id, email, name, role, created_at, password_hash FROM users WHERE email=$1`, normEmail(email)).
		Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &hash)
	if err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

func (s *Store) InsertUser(ctx context.Context, u *User, passwordHash string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (id,email,password_hash,name,role,created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, u.Email, passwordHash, u.Name, u.Role, u.CreatedAt)
	return err
}

func (s *Store) SetPassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, userID, passwordHash)
	return err
}

func (s *Store) SetPasswordAndRole(ctx context.Context, userID int64, passwordHash, role string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash=$2, role=$3 WHERE id=$1`, userID, passwordHash, role)
	return err
}

// --- links ---------------------------------------------------------------

const linkCols = `id, code, long_url, title, user_id, custom, redirect_type, expires_at, created_at, clicks, active`

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

func (s *Store) InsertLink(ctx context.Context, l *Link) error {
	l.Shard = s.ShardFor(l.Code)
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, shardTable(l.Shard), linkCols)
	_, err := s.pool.Exec(ctx, q, l.ID, l.Code, l.LongURL, l.Title, l.UserID, l.Custom, l.Redirect,
		l.ExpiresAt, l.CreatedAt, l.Clicks, l.Active)
	return err
}

// GetByCode reads one link from its shard. Returns pgx.ErrNoRows if missing.
func (s *Store) GetByCode(ctx context.Context, code string) (*Link, error) {
	shard := s.ShardFor(code)
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE code=$1`, linkCols, shardTable(shard))
	return scanLink(s.pool.QueryRow(ctx, q, code), shard)
}

func (s *Store) CodeExists(ctx context.Context, code string) (bool, error) {
	q := fmt.Sprintf(`SELECT 1 FROM %s WHERE code=$1`, shardTable(s.ShardFor(code)))
	var one int
	err := s.pool.QueryRow(ctx, q, code).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// queryEveryShard runs the same query (with %s for the table name) on every
// shard and concatenates the links.
func (s *Store) queryEveryShard(ctx context.Context, queryFmt string, args ...any) ([]*Link, error) {
	out := []*Link{}
	for shard := 0; shard < s.ring.ShardCount(); shard++ {
		rows, err := s.pool.Query(ctx, fmt.Sprintf(queryFmt, shardTable(shard)), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			l, err := scanLink(rows, shard)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, l)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ListByUser is a scatter-gather: take the newest `limit` from each shard,
// then merge-sort and cut to `limit` overall.
func (s *Store) ListByUser(ctx context.Context, userID int64, limit int) ([]*Link, error) {
	out, err := s.queryEveryShard(ctx,
		`SELECT `+linkCols+` FROM %s WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// AllCodes returns every link, busiest first within each shard (used to warm
// the cache and the Bloom filter at boot).
func (s *Store) AllCodes(ctx context.Context) ([]*Link, error) {
	return s.queryEveryShard(ctx, `SELECT `+linkCols+` FROM %s ORDER BY clicks DESC`)
}

// ReassignLinks moves the given codes to a new owner, one UPDATE per shard.
func (s *Store) ReassignLinks(ctx context.Context, codes []string, userID int64) error {
	for shard, cs := range s.groupByShard(codes) {
		q := fmt.Sprintf(`UPDATE %s SET user_id=$1 WHERE code = ANY($2)`, shardTable(shard))
		if _, err := s.pool.Exec(ctx, q, userID, cs); err != nil {
			return err
		}
	}
	return nil
}

// DeleteLink deletes code if userID owns it, and reports whether it did.
func (s *Store) DeleteLink(ctx context.Context, code string, userID int64) (bool, error) {
	q := fmt.Sprintf(`DELETE FROM %s WHERE code=$1 AND user_id=$2`, shardTable(s.ShardFor(code)))
	tag, err := s.pool.Exec(ctx, q, code, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ToggleActive flips the flag in one statement. pgx.ErrNoRows means the link
// doesn't exist or isn't owned by userID.
func (s *Store) ToggleActive(ctx context.Context, code string, userID int64) (*Link, error) {
	shard := s.ShardFor(code)
	q := fmt.Sprintf(`UPDATE %s SET active = NOT active WHERE code=$1 AND user_id=$2 RETURNING %s`, shardTable(shard), linkCols)
	return scanLink(s.pool.QueryRow(ctx, q, code, userID), shard)
}

// --- click counters (write-behind) ---------------------------------------

// BumpClicks records n clicks in memory. One UPDATE per redirect would
// limit redirects to Postgres write throughput, so flushClicks writes them
// in batches.
func (s *Store) BumpClicks(code string, n int64) {
	s.pendingMu.Lock()
	s.pendingClicks[code] += n
	s.pendingMu.Unlock()
}

// DropPending throws away buffered clicks for a deleted code, so they can't
// land on a new link that reuses the alias.
func (s *Store) DropPending(code string) {
	s.pendingMu.Lock()
	delete(s.pendingClicks, code)
	s.pendingMu.Unlock()
}

// SetClicks overwrites stored click totals (used at boot to match seeded
// links to their rebuilt click history) and drops buffered bumps for them.
func (s *Store) SetClicks(ctx context.Context, totals map[string]int64) error {
	for code, n := range totals {
		s.DropPending(code)
		q := fmt.Sprintf(`UPDATE %s SET clicks=$2 WHERE code=$1`, shardTable(s.ShardFor(code)))
		if _, err := s.pool.Exec(ctx, q, code, n); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) StartClickFlusher(ctx context.Context) {
	go func() {
		t := time.NewTicker(clickFlushInterval)
		defer t.Stop()
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

// flushClicks swaps the pending map for an empty one (so redirects are
// blocked only for that swap), then writes it out in chunks per shard.
// Chunks that fail are added back and retried next time.
func (s *Store) flushClicks(ctx context.Context) {
	s.pendingMu.Lock()
	pending := s.pendingClicks
	s.pendingClicks = map[string]int64{}
	s.pendingMu.Unlock()
	if len(pending) == 0 {
		return
	}

	codes := make([]string, 0, len(pending))
	for c := range pending {
		codes = append(codes, c)
	}
	var failed []string
	for shard, shardCodes := range s.groupByShard(codes) {
		for start := 0; start < len(shardCodes); start += flushChunk {
			chunk := shardCodes[start:min(start+flushChunk, len(shardCodes))]
			q, args := clickUpdateSQL(shardTable(shard), chunk, pending)
			if _, err := s.pool.Exec(ctx, q, args...); err != nil {
				log.Printf("click flush to %s failed, will retry: %v", shardTable(shard), err)
				failed = append(failed, chunk...)
			}
		}
	}

	if len(failed) == 0 {
		return
	}
	s.pendingMu.Lock()
	for _, c := range failed {
		s.pendingClicks[c] += pending[c]
	}
	s.pendingMu.Unlock()
}

// clickUpdateSQL builds one UPDATE for many rows:
//
//	UPDATE links_s0 AS l SET clicks = l.clicks + v.n
//	FROM (VALUES ($1,$2), ($3,$4), ...) AS v(code, n)
//	WHERE l.code = v.code
func clickUpdateSQL(table string, codes []string, counts map[string]int64) (string, []any) {
	var sb strings.Builder
	args := make([]any, 0, 2*len(codes))
	fmt.Fprintf(&sb, "UPDATE %s AS l SET clicks = l.clicks + v.n FROM (VALUES ", table)
	for i, c := range codes {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "($%d::text,$%d::bigint)", len(args)+1, len(args)+2)
		args = append(args, c, counts[c])
	}
	sb.WriteString(") AS v(code,n) WHERE l.code = v.code")
	return sb.String(), args
}

// --- stats ---------------------------------------------------------------

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

// ShardDistribution counts links and clicks per shard. count(*) on every
// shard is slow, so the answer is cached for statsTTL.
func (s *Store) ShardDistribution(ctx context.Context) []map[string]any {
	s.shardStatsMu.Lock()
	defer s.shardStatsMu.Unlock()
	if s.shardStats != nil && time.Since(s.shardStatsAt) < statsTTL {
		return s.shardStats
	}
	out := []map[string]any{}
	for i := 0; i < s.ring.ShardCount(); i++ {
		var links, clicks int64
		_ = s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*), COALESCE(sum(clicks),0) FROM %s`, shardTable(i))).Scan(&links, &clicks)
		out = append(out, map[string]any{"shard": i, "table": shardTable(i), "links": links, "clicks": clicks})
	}
	s.shardStats, s.shardStatsAt = out, time.Now()
	return out
}
