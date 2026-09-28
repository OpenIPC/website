package vendorfw

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Firmware is one downloadable file.
type Firmware struct {
	Key         string     `json:"key"`
	Version     string     `json:"version"`
	Build       string     `json:"build"`
	URL         string     `json:"url"`
	SHA256      *string    `json:"sha256"`
	Size        *int64     `json:"size"`
	PublishedAt *time.Time `json:"published_at"`
	SoC         *string    `json:"soc,omitempty"`
	// Origin is the archive the file was mirrored from when it is not the
	// vendor's own download page; OriginURL is the file's page there.
	Origin    *string `json:"origin,omitempty"`
	OriginURL *string `json:"origin_url,omitempty"`
}

// Device is what a device ID can be flashed with: every stock build the
// vendor published for it, newest first -- people move between them when one
// has a bug -- and the coupler image. Either may be empty.
type Device struct {
	ID    string     `json:"id"`
	Stock []Firmware `json:"stock"`
	// Sellers are builds a seller publishes (cctvsp.ru's IPeye builds),
	// offered only when the vendor has no build for the device: they are not
	// stock, and never stand in for it.
	Sellers []Firmware `json:"sellers"`
	Coupler *Firmware  `json:"coupler"`
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ForDevices answers, for each device ID asked, its stock builds (newest
// published first; the same file listed once however many catalogue rows
// carry it), a seller's builds when there is no stock one, and its newest
// coupler image.
func ForDevices(ctx context.Context, db querier, ids []string) (map[string]*Device, error) {
	out := map[string]*Device{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `
		SELECT device_id, source, key, version, build, asset_url, sha256, size, published_at, soc, origin, origin_url
		FROM vendor_firmware WHERE device_id = ANY($1)
		ORDER BY device_id, source, published_at DESC NULLS LAST, version DESC, key`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[[2]string]bool{} // (device ID, sha256): a file once per device
	for rows.Next() {
		var id, source string
		var f Firmware
		if err := rows.Scan(&id, &source, &f.Key, &f.Version, &f.Build, &f.URL, &f.SHA256, &f.Size, &f.PublishedAt, &f.SoC, &f.Origin, &f.OriginURL); err != nil {
			return nil, err
		}
		d := out[id]
		if d == nil {
			d = &Device{ID: id, Stock: []Firmware{}, Sellers: []Firmware{}}
			out[id] = d
		}
		if source == "coupler" {
			if d.Coupler == nil {
				d.Coupler = &f
			}
			continue
		}
		if f.SHA256 != nil {
			k := [2]string{id, *f.SHA256}
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		if f.Origin != nil {
			d.Sellers = append(d.Sellers, f)
		} else {
			d.Stock = append(d.Stock, f)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, d := range out {
		if len(d.Stock) > 0 {
			d.Sellers = []Firmware{}
		}
	}
	return out, nil
}

// BoardDevicesSQL lists (model_id, device_id): the catalogue boards known to
// run each device ID. A source may say so (board_device_ids: tehno32's
// firmware pages, cctvsp), or the vendor did, naming a device's firmware
// after the board itself -- NBD7024H-P's firmware is called NBD7024H-P. That
// second kind is read from the pushed lists, so it follows each push; only an
// exact code counts, normalised as boards.NormCode normalises the aliases.
// Only the vendor's own firmware names a board: a coupler image is named by
// OpenIPC after the build it replaces, and is not evidence of a board.
const BoardDevicesSQL = `
	SELECT model_id, device_id FROM board_device_ids
	UNION
	SELECT a.model_id, v.device_id FROM vendor_firmware v
	JOIN board_model_aliases a ON a.code_norm = btrim(regexp_replace(regexp_replace(upper(btrim(v.build)),
		'[[:space:]_/.]+', '-', 'g'), '-{2,}', '-', 'g'), '-')
	WHERE v.source = 'xmupdates'`

// API serves GET /api/v1/vendor-firmware/{deviceId}: what a visitor who read
// the device ID off their camera can flash, and which boards run it.
type API struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
}

func (a *API) Handlers() map[string]http.Handler {
	return map[string]http.Handler{"GET /api/v1/vendor-firmware/{deviceId}": http.HandlerFunc(a.device)}
}

type boardRef struct {
	ID    string  `json:"id"`
	Model *string `json:"model"`
	// Kind is board, or the finished device (camera, recorder, ...) that
	// runs the device ID: the site counts the two apart.
	Kind string `json:"kind"`
}

func (a *API) device(w http.ResponseWriter, r *http.Request) {
	id := DeviceID(r.PathValue("deviceId"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not an 8-character XM device ID"})
		return
	}
	ctx := r.Context()
	found, err := ForDevices(ctx, a.DB, []string{id})
	if err != nil {
		a.Log.Error("vendorfw: device", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	d := found[id]
	if d == nil {
		d = &Device{ID: id, Stock: []Firmware{}, Sellers: []Firmware{}}
	}
	boards := []boardRef{}
	rows, err := a.DB.Query(ctx, `
		SELECT DISTINCT m.id, m.model, m.kind FROM (`+BoardDevicesSQL+`) d JOIN board_models m ON m.id = d.model_id
		WHERE d.device_id = $1 ORDER BY m.id`, id)
	if err == nil {
		for rows.Next() {
			var b boardRef
			if err = rows.Scan(&b.ID, &b.Model, &b.Kind); err != nil {
				break
			}
			boards = append(boards, b)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
	}
	if err != nil {
		a.Log.Error("vendorfw: device boards", "id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "try again"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_ = jsonEncode(w, map[string]any{"schema": 1, "device": d, "boards": boards})
}
