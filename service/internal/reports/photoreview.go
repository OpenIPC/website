package reports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
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
var ErrNotPhoto = errors.New("only a photo is blurred, and only a photo or a flash dump kept private, this way")

// ErrNotConsented: a flash dump its sender did not share publicly is never
// published by a maintainer; they may only keep a public one private.
var ErrNotConsented = errors.New("its sender did not share this flash dump publicly")

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

// Blurrer makes a photo's blurred copy from its original (sum, as stored,
// of type mime) and says how to put it in the store. SetPhotoPublic calls
// it under the report's lock and the original's file lock, so the original
// cannot be taken out while it is read.
type Blurrer func(sum, mime string) (pc *PublicCopy, place func() error, err error)

// errBlur is a blur the request asked wrongly for (its regions), or that the
// original cannot give (it does not decode): answered 400.
type errBlur struct{ error }

// SetPhotoPublic records a reviewer's decision on photo position of report
// id. For PhotoBlur, blur makes the copy and its place puts it in the store
// under the copy's file lock, as Insert does; made is the copy, for the
// caller to remove if the decision then fails. It returns the public copy
// the photo had before when nothing names it any more, for the caller to
// delete once this is committed (RemoveUnreferenced).
func (s *Store) SetPhotoPublic(ctx context.Context, id string, position int, action string, regions []Region, by string, blur Blurrer) (made *PublicCopy, orphan string, err error) {
	if action != PhotoBlur && action != PhotoPrivate && action != PhotoAsSent {
		return nil, "", errors.New("a photo is blurred, kept private, or published as sent")
	}
	if (action == PhotoBlur) != (blur != nil) {
		return nil, "", errors.New("a blur comes with its blurrer, and only a blur")
	}
	if action != PhotoBlur {
		regions = []Region{}
	}
	rj, err := json.Marshal(regions)
	if err != nil {
		return nil, "", err
	}
	err = Unguarded(ctx, s.DB, func(tx pgx.Tx) error {
		// The report's lock, as an edit and a review hold it: a photo its
		// sender is taking out is not given a copy at the same time.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('report-stars:' || $1, 0))`, id); err != nil {
			return err
		}
		var kind, sum, mime string
		var bytes int64
		var old *string
		err := tx.QueryRow(ctx, `SELECT kind, sha256, mime, bytes, public_sha256 FROM report_files WHERE report_id = $1 AND position = $2`,
			id, position).Scan(&kind, &sum, &mime, &bytes, &old)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// A flash dump its sender shared publicly can still hold what
		// identifies the camera -- a vendor-cloud ID in a raw read
		// (r-kebw28ys) -- so a maintainer may keep it private, and give it
		// back as sent; never blur it, never publish one not shared.
		if kind == "backup" && action != PhotoBlur {
			if action == PhotoAsSent {
				var consent string
				if err := tx.QueryRow(ctx, `SELECT backup_consent FROM reports WHERE id = $1`, id).Scan(&consent); err != nil {
					return err
				}
				if consent != "public" {
					return ErrNotConsented
				}
			}
		} else if kind != "photo" {
			return ErrNotPhoto
		}
		var pub, pubMime *string
		var pubBytes *int64
		switch action {
		case PhotoAsSent:
			pub, pubBytes = &sum, &bytes
		case PhotoBlur:
			// The original's lock: RemoveUnreferenced, after an edit took
			// another report's copy of these bytes out, waits for this read.
			if _, err := tx.Exec(ctx, lockFileSh, sum); err != nil {
				return err
			}
			pc, place, err := blur(sum, mime)
			if err != nil {
				return err
			}
			made = pc
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
	return made, orphan, err
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
	var in struct {
		Action  string   `json:"action"`
		Regions []Region `json:"regions"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"action": "blur", "regions": [{"x": 0.1, "y": 0.2, "w": 0.3, "h": 0.1}]}, {"action": "private"} or {"action": "as_sent"}`)
		return false
	}
	err := a.DecidePhoto(r.Context(), id, position, in.Action, in.Regions, by)
	var bad errBlur
	switch {
	case errors.As(err, &bad):
		a.refuse(w, http.StatusBadRequest, bad.Error())
		return false
	case errors.Is(err, ErrNotFound):
		a.refuse(w, http.StatusNotFound, "the report has no such file")
		return false
	case errors.Is(err, ErrNotPhoto), errors.Is(err, ErrNotConsented):
		a.refuse(w, http.StatusBadRequest, err.Error())
		return false
	case err != nil:
		a.fail(w, "the photo's public copy", err)
		return false
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "position": position, "public": publicState(in.Action)})
	return true
}

// DecidePhoto is a reviewer's decision on photo position of report id --
// PhotoBlur with its regions, PhotoPrivate or PhotoAsSent -- the page's
// (ReviewPhoto) and `openipc reports photo`'s. A request the photo cannot
// answer is an errBlur (400 on the page), a file the report does not have
// ErrNotFound, one that is no photo ErrNotPhoto.
func (a *API) DecidePhoto(ctx context.Context, id string, position int, action string, regions []Region, by string) error {
	if action != PhotoBlur && action != PhotoPrivate && action != PhotoAsSent {
		return errBlur{errors.New("action is blur, private (not the sender's unit: never published) or as_sent")}
	}
	if action == PhotoBlur {
		// What can be refused without the photo is refused before its lock.
		if err := validRegions(regions); err != nil {
			return errBlur{err}
		}
	}
	st := a.store()
	var blur Blurrer
	if action == PhotoBlur {
		blur = func(sum, orig string) (*PublicCopy, func() error, error) {
			src, err := a.Files.Read(sum)
			if errors.Is(err, fs.ErrNotExist) {
				// Gone from the store although a row names it: the
				// nightly verify reports it; this photo cannot be blurred.
				return nil, nil, ErrNotFound
			}
			if err != nil {
				return nil, nil, err
			}
			out, mt, err := BlurPhoto(src, regions)
			if err != nil {
				return nil, nil, errBlur{err}
			}
			h := sha256.Sum256(out)
			pc := &PublicCopy{SHA256: hex.EncodeToString(h[:]), Bytes: int64(len(out))}
			if orig != mt {
				pc.Mime = mt
			}
			return pc, func() error { _, err := a.Files.Put(out); return err }, nil
		}
	}
	pc, orphan, err := st.SetPhotoPublic(ctx, id, position, action, regions, by, blur)
	if err != nil {
		if pc != nil {
			// A copy placed before the decision failed belongs to no row.
			_, _ = st.RemoveUnreferenced(ctx, a.Files, pc.SHA256)
		}
		return err
	}
	if orphan != "" {
		if _, err := st.RemoveUnreferenced(ctx, a.Files, orphan); err != nil {
			a.Log.Warn("reports: a replaced public copy stays", "report", id, "sha256", orphan, "err", err)
		}
	}
	a.Log.Info("reports: photo reviewed", "report", id, "position", position, "action", action, "regions", len(regions), "by", by)
	return nil
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
