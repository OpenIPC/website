// Package db owns the PostgreSQL connection and the schema.
//
// The schema is SQL files embedded in the binary and applied by Migrate, in
// order, each in its own transaction, under an advisory lock so two deploys
// cannot interleave. `serve` refuses to start against a database that is behind
// the binary: the deploy runs `openipc migrate` first, and a container that
// started anyway would answer every request with a missing column.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Advisory lock keys. Arbitrary, fixed, and distinct.
const (
	migrateLock   int64 = 0x6f70656e69706301 // "openipc" 01
	webRoleLock   int64 = 0x6f70656e69706302
	purgeLock     int64 = 0x6f70656e69706303
	versionsTable       = "service_migrations"
)

type migration struct {
	version int
	name    string
	sql     string
}

func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %s has no NNN_ prefix", e.Name())
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		body, err := migrationFiles.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migrations must be numbered 1..n without gaps; %s is out of place", m.name)
		}
	}
	return out, nil
}

// Latest is the schema version this binary expects.
func Latest() int {
	ms, err := migrations()
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].version
}

// Open connects and pings. Small pool: the whole site does a few thousand
// queries a day, and the host runs MariaDB beside this.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}
	if cfg.MaxConns > 8 {
		cfg.MaxConns = 8
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// Migrate applies every migration the database has not seen and returns the
// versions it applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]int, error) {
	ms, err := migrations()
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLock); err != nil {
		return nil, err
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrateLock) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+versionsTable+` (
		version    integer PRIMARY KEY,
		name       text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return nil, err
	}
	current, err := version(ctx, conn.Conn())
	if err != nil {
		return nil, err
	}
	var applied []int
	for _, m := range ms {
		if m.version <= current {
			continue
		}
		err := pgx.BeginFunc(ctx, conn.Conn(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return fmt.Errorf("%s: %w", m.name, err)
			}
			_, err := tx.Exec(ctx, "INSERT INTO "+versionsTable+" (version, name) VALUES ($1, $2)", m.version, m.name)
			return err
		})
		if err != nil {
			return applied, err
		}
		applied = append(applied, m.version)
	}
	return applied, nil
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func version(ctx context.Context, q querier) (int, error) {
	var v int
	err := q.QueryRow(ctx, "SELECT coalesce(max(version), 0) FROM "+versionsTable).Scan(&v)
	return v, err
}

// CheckCurrent is the startup guard: the database must be at least at the
// version this binary was built for, or the deploy skipped `openipc migrate`.
// Ahead is allowed: that is an image rollback, which never rolls the schema
// back, and migrations are kept additive so the older binary still reads it.
func CheckCurrent(ctx context.Context, pool *pgxpool.Pool) error {
	v, err := version(ctx, pool)
	if err != nil {
		return fmt.Errorf("reading schema version (has `openipc migrate` run?): %w", err)
	}
	if want := Latest(); v < want {
		return fmt.Errorf("database schema is at %d, this binary needs %d: run `openipc migrate`", v, want)
	}
	return nil
}

// Singleton holds a session-level advisory lock for as long as the process
// lives, on a connection kept out of the pool. It is how "exactly one web
// process" is enforced rather than documented: the variant queue and the
// in-memory limits assume it.
type Singleton struct{ conn *pgxpool.Conn }

func ClaimWebRole(ctx context.Context, pool *pgxpool.Pool) (*Singleton, error) {
	return claim(ctx, pool, webRoleLock, "another `openipc serve --role web` holds this database")
}

func claim(ctx context.Context, pool *pgxpool.Pool, key int64, busy string) (*Singleton, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var ok bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&ok); err != nil {
		conn.Release()
		return nil, err
	}
	if !ok {
		conn.Release()
		return nil, fmt.Errorf("%s", busy)
	}
	return &Singleton{conn: conn}, nil
}

func (s *Singleton) Release() {
	if s == nil || s.conn == nil {
		return
	}
	s.conn.Exec(context.Background(), "SELECT pg_advisory_unlock_all()") //nolint:errcheck
	s.conn.Release()
}

// TryPurgeLock is taken by `openipc purge`: a second run while one is going is
// a no-op rather than a race.
func TryPurgeLock(ctx context.Context, pool *pgxpool.Pool) (*Singleton, error) {
	return claim(ctx, pool, purgeLock, "another purge is running")
}
