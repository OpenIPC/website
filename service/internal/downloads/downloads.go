// Package downloads is the download stats: one row per firmware image a
// visitor started to download. Kept indefinitely; there is no retention.
package downloads

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ DB *pgxpool.Pool }

type Row struct {
	SoCModel  string
	FlashType string
	Release   string
	FlashSize int
	Bytes     int64
}

func (s *Store) Record(ctx context.Context, r Row) error {
	var size *int
	if r.FlashSize > 0 {
		size = &r.FlashSize
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO downloads (soc_model, flash_type, release, flash_size, bytes)
		VALUES ($1, $2, $3, $4, $5)`, r.SoCModel, r.FlashType, r.Release, size, r.Bytes)
	return err
}

var (
	bytesUnit  = regexp.MustCompile(`(?i)^bytes\s*=`)
	firstRange = regexp.MustCompile(`(?i)^bytes\s*=\s*0-`)
)

// FirstChunk says whether a request is the start of a download rather than
// the same download continuing. A browser or download manager fetches an
// 8-32 MB image in chunks, one request each; counting requests counted 1,203
// rows for 824 real downloads. So: a GET with no Range counts, a Range that
// starts at byte 0 counts, and a Range in a unit the file server ignores
// counts (it is answered with the whole file). A suffix range, any other
// range, and HEAD do not.
func FirstChunk(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	rng := strings.TrimSpace(r.Header.Get("Range"))
	if rng == "" || !bytesUnit.MatchString(rng) {
		return true
	}
	return firstRange.MatchString(rng)
}
