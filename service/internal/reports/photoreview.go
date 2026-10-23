package reports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
)

// A reviewer's word on one photo (migration 038): its public copy blurred
// where a label or a code shows, no public copy at all for a picture that is
// not the sender's unit, or the photo as sent again. The original is never
// touched; the row's public_sha256 names what is served once the report is
// published, and every decision is logged in report_photo_reviews.

// Photo actions.
const (
	PhotoBlur    = "blur"
	PhotoPrivate = "private"
	PhotoAsSent  = "as_sent"
)

// ErrNotPhoto is a photo decision on a file that is not a photo.
var ErrNotPhoto = errors.New("only a photo is blurred or kept private this way")

// ownPhoto is the own_photo column: true, or NULL when the sender was not
// asked or did not say so.
func ownPhoto(f File) *bool {
	if f.Kind != "photo" || !f.Own {
		return nil
	}
	t := true
	return &t
}

// PublicCopy is a photo's new public copy, already made.
type PublicCopy struct {
	SHA256 string
	Bytes  int64
	// Mime: the copy's type, only when it is not the original's.
	Mime string
}

// SetPhotoPublic records a reviewer's decision on photo position of report
// id: pc is the blurred copy for PhotoBlur, nil otherwise, and place puts it
// in the store under the file's lock, as Insert does. It returns the public
// copy the photo had before when nothing names it any more, for the caller
// to delete once this is committed (RemoveUnreferenced).
func (s *Store) SetPhotoPublic(ctx context.Context, id string, position int, action string, regions []Region, by string, pc *PublicCopy, place func() error) (orphan string, err error) {
	if action != PhotoBlur && action != PhotoPrivate && action != PhotoAsSent {
		return "", errors.New("a photo is blurred, kept private, or published as sent")
	}
	if (action == PhotoBlur) != (pc != nil) {
		return "", errors.New("a blur comes with its copy, and only a blur")
	}
	if action != PhotoBlur {
		regions = []Region{}
	}
	rj, err := json.Marshal(regions)
	if err != nil {
		return "", err
	}
	err = Unguarded(ctx, s.DB, func(tx pgx.Tx) error {
		// The report's lock, as an edit and a review hold it: a photo its
		// sender is taking out is not given a copy at the same time.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		var kind, sum string
		var bytes int64
		var old *string
		err := tx.QueryRow(ctx, `SELECT kind, sha256, bytes, public_sha256 FROM report_files WHERE report_id = $1 AND position = $2`,
			id, position).Scan(&kind, &sum, &bytes, &old)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if kind != "photo" {
			return ErrNotPhoto
		}
		var pub, pubMime *string
		var pubBytes *int64
		switch action {
		case PhotoAsSent:
			pub, pubBytes = &sum, &bytes
		case PhotoBlur:
			if _, err := tx.Exec(ctx, lockFileSh, pc.SHA256); err != nil {
				return err
			}
			if err := place(); err != nil {
				return err
			}
			pub, pubBytes = &pc.SHA256, &pc.Bytes
			if pc.Mime != "" {
				pubMime = &pc.Mime
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE report_files SET public_sha256 = $3, public_bytes = $4, public_mime = $5
			WHERE report_id = $1 AND position = $2`, id, position, pub, pubBytes, pubMime); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO report_photo_reviews (report_id, position, action, regions, public_sha256, by)
			VALUES ($1, $2, $3, $4, $5, $6)`, id, position, action, rj, pub, by); err != nil {
			return err
		}
		if old != nil && *old != sum && (pub == nil || *old != *pub) {
			var used bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM report_files WHERE sha256 = $1 OR public_sha256 = $1)`, *old).Scan(&used); err != nil {
				return err
			}
			if !used {
				orphan = *old
			}
		}
		return nil
	})
	return orphan, err
}

// blurs is the regions of each photo whose last decision was a blur, as the
// reviewer drew them, for the next one to start from.
func (s *Store) blurs(ctx context.Context, id string) (map[int][]Region, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT DISTINCT ON (position) position, action, regions FROM report_photo_reviews
		WHERE report_id = $1 ORDER BY position, id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]Region{}
	for rows.Next() {
		var pos int
		var action string
		var rj []byte
		if err := rows.Scan(&pos, &action, &rj); err != nil {
			return nil, err
		}
		if action == PhotoBlur {
			var rs []Region
			_ = json.Unmarshal(rj, &rs)
			out[pos] = rs
		}
	}
	return out, rows.Err()
}

// PublicStored is a file's public copy, for its sender or a maintainer to
// see what will be published: ErrNotFound when it has none.
func (s *Store) PublicStored(ctx context.Context, id string, position int) (sum, name, mime, kind string, err error) {
	var pub *string
	var pubMime string
	err = s.DB.QueryRow(ctx, `SELECT public_sha256, name, mime, coalesce(public_mime, ''), kind FROM report_files WHERE report_id = $1 AND position = $2`,
		id, position).Scan(&pub, &name, &mime, &pubMime, &kind)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && pub == nil {
		return "", "", "", "", ErrNotFound
	}
	if err != nil {
		return
	}
	sum = *pub
	if pubMime != "" {
		mime, name = pubMime, publicName(name, pubMime)
	}
	return
}

// Read is a stored file's bytes.
func (s *Files) Read(sum string) ([]byte, error) {
	if !hexSum.MatchString(sum) {
		return nil, errors.New("not a sha256")
	}
	return os.ReadFile(s.path(sum))
}

// ReviewPhoto is POST /api/v1/club/review/{id}/files/{position}
// {"action": "blur", "regions": [{"x", "y", "w", "h"}...]},
// {"action": "private"} or {"action": "as_sent"}, for a maintainer
// (internal/club decides who). A blur is made here, from the original and
// the regions, never taken from the browser. It answers itself and says
// whether the decision was made.
func (a *API) ReviewPhoto(w http.ResponseWriter, r *http.Request, id string, position int, by string) bool {
	ctx := r.Context()
	var in struct {
		Action  string   `json:"action"`
		Regions []Region `json:"regions"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"action": "blur", "regions": [{"x": 0.1, "y": 0.2, "w": 0.3, "h": 0.1}]}, {"action": "private"} or {"action": "as_sent"}`)
		return false
	}
	if in.Action != PhotoBlur && in.Action != PhotoPrivate && in.Action != PhotoAsSent {
		a.refuse(w, http.StatusBadRequest, "action is blur, private (not the sender's unit: never published) or as_sent")
		return false
	}
	st := a.store()
	var pc *PublicCopy
	var place func() error
	if in.Action == PhotoBlur {
		sum, _, orig, kind, err := st.StoredFile(ctx, id, position)
		if errors.Is(err, ErrNotFound) {
			a.refuse(w, http.StatusNotFound, "the report has no such file")
			return false
		}
		if err != nil {
			a.fail(w, "the photo", err)
			return false
		}
		if kind != "photo" {
			a.refuse(w, http.StatusBadRequest, ErrNotPhoto.Error())
			return false
		}
		src, err := a.Files.Read(sum)
		if err != nil {
			a.fail(w, "the photo", err)
			return false
		}
		out, mt, err := BlurPhoto(src, in.Regions)
		if err != nil {
			a.refuse(w, http.StatusBadRequest, err.Error())
			return false
		}
		h := sha256.Sum256(out)
		pc = &PublicCopy{SHA256: hex.EncodeToString(h[:]), Bytes: int64(len(out))}
		if orig != mt {
			pc.Mime = mt
		}
		place = func() error { _, err := a.Files.Put(out); return err }
	}
	orphan, err := st.SetPhotoPublic(ctx, id, position, in.Action, in.Regions, by, pc, place)
	if err != nil && pc != nil {
		// A copy placed before the decision failed belongs to no row.
		_, _ = st.RemoveUnreferenced(ctx, a.Files, pc.SHA256)
	}
	switch {
	case errors.Is(err, ErrNotFound):
		a.refuse(w, http.StatusNotFound, "the report has no such file")
		return false
	case errors.Is(err, ErrNotPhoto):
		a.refuse(w, http.StatusBadRequest, err.Error())
		return false
	case err != nil:
		a.fail(w, "the photo's public copy", err)
		return false
	}
	if orphan != "" {
		if _, err := st.RemoveUnreferenced(ctx, a.Files, orphan); err != nil {
			a.Log.Warn("reports: a replaced public copy stays", "report", id, "sha256", orphan, "err", err)
		}
	}
	a.Log.Info("reports: photo reviewed", "report", id, "position", position, "action", in.Action, "regions", len(in.Regions), "by", by)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "position": position, "public": publicState(in.Action)})
	return true
}

// publicState is what MemberFile.Public says of a photo after action.
func publicState(action string) string {
	if action == PhotoBlur {
		return "blurred"
	}
	return action
}

// ServePublicCopy is a file's public copy -- a blurred photo -- for its
// sender or a maintainer, before or after the report is published.
func (a *API) ServePublicCopy(w http.ResponseWriter, r *http.Request, id string, position int) {
	sum, name, mt, kind, err := a.store().PublicStored(r.Context(), id, position)
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, "the file", err)
		return
	}
	a.send(w, id, sum, name, mt, kind, "private, no-store")
}
