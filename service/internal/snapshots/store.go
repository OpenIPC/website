package snapshots

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PublicIDFormat is what a snapshot's address looks like.
var PublicIDFormat = regexp.MustCompile(`^[0-9a-f]{20}$`)

var allDigits = regexp.MustCompile(`^[0-9]+$`)

// Snapshot is one row, as the wall reads it.
type Snapshot struct {
	ID                  int64
	PublicID            string
	MACKey              string
	CameraToken         string
	Caption             *string
	Firmware            *string
	FlashSize           *string
	Hostname            *string
	Sensor              *string
	SoC                 *string
	SoCTemperature      *string
	Streamer            *string
	Uptime              *string
	ContentType         string
	ByteSize            int64
	Width               *int32
	Height              *int32
	VariantsGeneratedAt *time.Time
	CreatedAt           time.Time
}

const columns = `id, public_id, mac_key, camera_token, caption, firmware, flash_size, hostname,
	sensor, soc, soc_temperature, streamer, uptime, content_type, byte_size, width, height,
	variants_generated_at, created_at`

func scan(row pgx.Row) (*Snapshot, error) {
	s := &Snapshot{}
	err := row.Scan(&s.ID, &s.PublicID, &s.MACKey, &s.CameraToken, &s.Caption, &s.Firmware, &s.FlashSize,
		&s.Hostname, &s.Sensor, &s.SoC, &s.SoCTemperature, &s.Streamer, &s.Uptime, &s.ContentType,
		&s.ByteSize, &s.Width, &s.Height, &s.VariantsGeneratedAt, &s.CreatedAt)
	return s, err
}

func scanAll(rows pgx.Rows) ([]*Snapshot, error) {
	defer rows.Close()
	var out []*Snapshot
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Store is the snapshots table.
type Store struct {
	DB       *pgxpool.Pool
	TokenKey string // secret_key_base
}

// CameraToken is HMAC-SHA256 over the canonical MAC with CAMERA_TOKEN_KEY,
// first sixteen hex characters. The key and the formula are unchanged since
// the first camera links were shared, so those links still resolve.
func (st *Store) CameraToken(mac string) string {
	m := hmac.New(sha256.New, []byte(st.TokenKey))
	m.Write([]byte(MACKey(mac)))
	return hex.EncodeToString(m.Sum(nil))[:16]
}

// NewPublicID is twenty hex characters and never all digits.
func NewPublicID() string {
	for {
		var b [10]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		id := hex.EncodeToString(b[:])
		if !allDigits.MatchString(id) {
			return id
		}
	}
}

// SecondsSinceLast answers the interval question by the database's clock,
// which is the clock the rows were stamped with. ok is false for a camera
// with no frames.
func (st *Store) SecondsSinceLast(ctx context.Context, mac string) (float64, bool, error) {
	var elapsed float64
	err := st.DB.QueryRow(ctx, `SELECT extract(epoch FROM now() - created_at)::float8
		FROM snapshots WHERE mac_key = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, MACKey(mac)).Scan(&elapsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return elapsed, err == nil, err
}

// NewRow is what Insert writes.
type NewRow struct {
	PublicID    string
	MAC         string
	IP          string
	Attributes  map[string]*string
	ContentType string
	ByteSize    int64
}

// ErrTooSoon is the interval refusing a frame; Elapsed is the seconds since
// the camera's last one, by the database's clock.
type ErrTooSoon struct{ Elapsed float64 }

func (e ErrTooSoon) Error() string {
	return fmt.Sprintf("too soon: %.0f s since the last frame", e.Elapsed)
}

// InsertIfDue is the interval check and the insert as one step. Two uploads
// from one camera arriving together would otherwise both read the same last
// frame, both pass, and both be stored: the check and the write happen under
// a transaction-scoped advisory lock on the camera's key, so the second waits
// for the first and then sees it. minElapsed <= 0 skips the check (a
// whitelisted address).
func (st *Store) InsertIfDue(ctx context.Context, row NewRow, minElapsed float64) error {
	return pgx.BeginFunc(ctx, st.DB, func(tx pgx.Tx) error {
		key := MACKey(row.MAC)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('snapshot:' || $1, 0))`, key); err != nil {
			return err
		}
		if minElapsed > 0 {
			var elapsed float64
			err := tx.QueryRow(ctx, `SELECT extract(epoch FROM now() - created_at)::float8
				FROM snapshots WHERE mac_key = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, key).Scan(&elapsed)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if err == nil && elapsed < minElapsed {
				return ErrTooSoon{Elapsed: elapsed}
			}
		}
		return insert(ctx, tx, st, row)
	})
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Insert writes a row with no interval check.
func (st *Store) Insert(ctx context.Context, row NewRow) error { return insert(ctx, st.DB, st, row) }

func insert(ctx context.Context, db execer, st *Store, row NewRow) error {
	a := row.Attributes
	_, err := db.Exec(ctx, `INSERT INTO snapshots
		(public_id, mac_address, camera_token, ip_address, caption, firmware, flash_size, hostname,
		 sensor, soc, soc_temperature, streamer, uptime, content_type, byte_size)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		row.PublicID, row.MAC, st.CameraToken(row.MAC), row.IP,
		a["caption"], a["firmware"], a["flash_size"], a["hostname"], a["sensor"], a["soc"],
		a["soc_temperature"], a["streamer"], a["uptime"], row.ContentType, row.ByteSize)
	return err
}

// IsUniqueViolation tells a public_id collision from a real failure.
func IsUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// LatestPerCamera is the newest frame of every camera seen in the last day,
// newest first. DISTINCT ON walks snapshots_by_camera, one group per camera.
func (st *Store) LatestPerCamera(ctx context.Context, limit int) ([]*Snapshot, error) {
	q := `SELECT ` + columns + ` FROM (
			SELECT DISTINCT ON (mac_key) * FROM snapshots
			WHERE created_at > now() - interval '1 day'
			ORDER BY mac_key, created_at DESC, id DESC
		) latest ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := st.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

// ByPublicID finds one frame; nil when there is none.
func (st *Store) ByPublicID(ctx context.Context, id string) (*Snapshot, error) {
	s, err := scan(st.DB.QueryRow(ctx, `SELECT `+columns+` FROM snapshots WHERE public_id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// ByCameraToken is the camera's newest frame; nil when there is none.
func (st *Store) ByCameraToken(ctx context.Context, token string) (*Snapshot, error) {
	s, err := scan(st.DB.QueryRow(ctx, `SELECT `+columns+` FROM snapshots WHERE camera_token = $1
		ORDER BY created_at DESC, id DESC LIMIT 1`, token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// DayOf is a camera's last day, newest first, limited when limit > 0.
func (st *Store) DayOf(ctx context.Context, subject *Snapshot, limit int) ([]*Snapshot, error) {
	q := `SELECT ` + columns + ` FROM snapshots
		WHERE mac_key = $1 AND created_at BETWEEN now() - interval '1 day' AND now()
		ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := st.DB.Query(ctx, q, subject.MACKey)
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

// Pending is every frame whose variants are not on disk yet, oldest first:
// the variant queue, recovered at boot.
func (st *Store) Pending(ctx context.Context) ([]string, error) {
	rows, err := st.DB.Query(ctx, `SELECT public_id FROM snapshots
		WHERE variants_generated_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Generated is every frame whose variants are done: the sweep removes any
// original still beside them (a crash between marking and unlinking).
func (st *Store) Generated(ctx context.Context) ([]string, error) {
	rows, err := st.DB.Query(ctx, `SELECT public_id FROM snapshots WHERE variants_generated_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkGenerated records the variants and the image's dimensions. It reports
// whether the row still exists: a frame purged while its variants were being
// made must have its files removed by the caller.
func (st *Store) MarkGenerated(ctx context.Context, publicID string, width, height int) (bool, error) {
	tag, err := st.DB.Exec(ctx, `UPDATE snapshots SET variants_generated_at = now(),
		width = $2, height = $3 WHERE public_id = $1`, publicID, width, height)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Exists is a cheap check for the worker.
func (st *Store) Exists(ctx context.Context, publicID string) (bool, error) {
	var ok bool
	err := st.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM snapshots WHERE public_id = $1)`, publicID).Scan(&ok)
	return ok, err
}
