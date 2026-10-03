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

	"github.com/OpenIPC/website/service/internal/keyframe"
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

// shown leaves out a frame the wall refused: its row stays, but it has no
// picture, and a card for it is a blank with "x" for its size that never
// paints. A frame still being published is shown; its picture follows.
const shown = `NOT (variants_generated_at IS NOT NULL AND width IS NULL)`

// LatestPerCamera is the newest shown frame of every camera seen in the last
// day, newest first. DISTINCT ON walks snapshots_by_camera, one group per
// camera.
func (st *Store) LatestPerCamera(ctx context.Context, limit int) ([]*Snapshot, error) {
	q := `SELECT ` + columns + ` FROM (
			SELECT DISTINCT ON (mac_key) * FROM snapshots
			WHERE created_at > now() - interval '1 day' AND ` + shown + `
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

// What the home page's mosaic leaves out (migration 022). Measured on the
// wall's 4,123 frames of 2026-10-03, where 309 fell under one of these and
// every one of them was a frame with nothing to see: flat grey or white with
// at most the camera's clock on it, a night scene with no light, or a garden
// camera blown out to white.
const (
	// ShowcaseMinSpread: the 5th to 95th percentile of brightness. Flat
	// frames measure 0-7; the dimmest picture still worth showing, about 24.
	ShowcaseMinSpread = 16
	// ShowcaseMaxMedian: half the frame at least this bright is blown out.
	ShowcaseMaxMedian = 245
	// ShowcaseMinBright: the brightest 5% darker than this is a black frame.
	ShowcaseMinBright = 20
	// ShowcaseMinAge: how long a camera has to have been uploading before its
	// frames reach the front page, so a new one cannot deface it.
	ShowcaseMinAge = 30 * 24 * time.Hour
	// ShowcaseMinDays: and on how many separate days. A camera that uploaded
	// once a month ago, or a MAC invented then, has one.
	ShowcaseMinDays = 20
)

// Showcase is LatestPerCamera for the home page: the newest measured frame of
// every camera seen in the last day, leaving out a camera that has been
// uploading for less than ShowcaseMinAge, or on fewer than ShowcaseMinDays
// days, or whose newest frame has nothing to see in it. Its newest frame, not its best one: a camera that has gone dark
// is not shown by a picture from before it did.
func (st *Store) Showcase(ctx context.Context, limit int) ([]*Snapshot, error) {
	q := `SELECT ` + columns + ` FROM (
			SELECT DISTINCT ON (s.mac_key) s.* FROM snapshots s
			JOIN cameras c ON c.mac_key = s.mac_key
			WHERE s.created_at > now() - interval '1 day' AND s.luma_p50 IS NOT NULL
			  AND c.first_seen <= now() - make_interval(secs => $1) AND c.days >= $5
			ORDER BY s.mac_key, s.created_at DESC, s.id DESC
		) latest
		WHERE luma_p95 - luma_p5 >= $2 AND luma_p50 < $3 AND luma_p95 >= $4
		ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := st.DB.Query(ctx, q, ShowcaseMinAge.Seconds(), ShowcaseMinSpread, ShowcaseMaxMedian, ShowcaseMinBright, ShowcaseMinDays)
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

// ByCameraToken is the camera's newest shown frame; nil when there is none.
func (st *Store) ByCameraToken(ctx context.Context, token string) (*Snapshot, error) {
	s, err := scan(st.DB.QueryRow(ctx, `SELECT `+columns+` FROM snapshots WHERE camera_token = $1 AND `+shown+`
		ORDER BY created_at DESC, id DESC LIMIT 1`, token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// DayOf is a camera's last day, newest first, limited when limit > 0.
func (st *Store) DayOf(ctx context.Context, subject *Snapshot, limit int) ([]*Snapshot, error) {
	q := `SELECT ` + columns + ` FROM snapshots
		WHERE mac_key = $1 AND created_at BETWEEN now() - interval '1 day' AND now() AND ` + shown + `
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

// Pending is every frame not yet published, oldest first:
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

// MarkGenerated records the variants, the image's dimensions and its
// brightness, and counts the frame toward its camera's history (cameras,
// migration 022) -- here and not on insert, so only a frame the wall accepted
// counts. It reports whether the row still exists: a frame purged while its
// variants were being made must have its files removed by the caller.
func (st *Store) MarkGenerated(ctx context.Context, publicID string, width, height int, luma keyframe.Luma) (bool, error) {
	var n int
	err := st.DB.QueryRow(ctx, `WITH frame AS (
			UPDATE snapshots SET variants_generated_at = now(),
				width = $2, height = $3, luma_p5 = $4, luma_p50 = $5, luma_p95 = $6
			WHERE public_id = $1 RETURNING mac_key, created_at
		), seen AS (
			INSERT INTO cameras (mac_key, first_seen, last_day, days)
			SELECT mac_key, created_at, (created_at AT TIME ZONE 'UTC')::date, 1 FROM frame
			ON CONFLICT (mac_key) DO UPDATE SET
				first_seen = LEAST(cameras.first_seen, EXCLUDED.first_seen),
				days = cameras.days + (EXCLUDED.last_day > cameras.last_day)::int,
				last_day = GREATEST(cameras.last_day, EXCLUDED.last_day)
		)
		SELECT count(*) FROM frame`,
		publicID, width, height, int16(luma.P5), int16(luma.P50), int16(luma.P95)).Scan(&n)
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Exists is a cheap check for the worker.
func (st *Store) Exists(ctx context.Context, publicID string) (bool, error) {
	var ok bool
	err := st.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM snapshots WHERE public_id = $1)`, publicID).Scan(&ok)
	return ok, err
}

// MarkRefused closes the row of an upload the wall will not publish: done, so
// neither the sweep nor the probe's stuck-queue count sees it again, and
// without dimensions, because there is no picture. The row itself stays --
// the camera was answered 201, and an accepted upload has a row.
func (st *Store) MarkRefused(ctx context.Context, publicID, reason string) error {
	_, err := st.DB.Exec(ctx, `UPDATE snapshots SET variants_generated_at = now(), refused_reason = $2
		WHERE public_id = $1`, publicID, reason)
	return err
}
