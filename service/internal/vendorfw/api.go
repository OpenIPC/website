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
}

// Device is what a device ID can be flashed with: every stock build the
// vendor published for it, newest first -- people move between them when one
// has a bug -- and the coupler image. Either may be empty.
type Device struct {
	ID      string     `json:"id"`
	Stock   []Firmware `json:"stock"`
	Coupler *Firmware  `json:"coupler"`
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ForDevices answers, for each device ID asked, its stock builds (newest
// published first; the same file listed once however many catalogue rows
// carry it) and its newest coupler image.
func ForDevices(ctx context.Context, db querier, ids []string) (map[string]*Device, error) {
	out := map[string]*Device{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `
		SELECT device_id, source, key, version, build, asset_url, sha256, size, published_at, soc
		FROM vendor_firmware WHERE device_id = ANY($1)
		ORDER BY device_id, source, published_at DESC NULLS LAST, version DESC, key`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var id, source string
		var f Firmware
		if err := rows.Scan(&id, &source, &f.Key, &f.Version, &f.Build, &f.URL, &f.SHA256, &f.Size, &f.PublishedAt, &f.SoC); err != nil {
			return nil, err
		}
		d := out[id]
		if d == nil {
			d = &Device{ID: id, Stock: []Firmware{}}
			out[id] = d
		}
		if source == "coupler" {
			if d.Coupler == nil {
				d.Coupler = &f
			}
			continue
		}
		if f.SHA256 != nil {
			if seen[*f.SHA256] {
				continue
			}
			seen[*f.SHA256] = true
		}
		d.Stock = append(d.Stock, f)
	}
	return out, rows.Err()
}

// BoardDevicesSQL lists (model_id, device_id): the catalogue boards known to
// run each device ID. A source may say so (board_device_ids: tehno32's
// firmware pages, cctvsp), or the vendor did, naming a device's firmware
// after the board itself -- NBD7024H-P's firmware is called NBD7024H-P. That
// second kind is read from the pushed lists, so it follows each push; only an
// exact code counts, normalised as the board aliases are.
const BoardDevicesSQL = `
	SELECT model_id, device_id FROM board_device_ids
	UNION
	SELECT a.model_id, v.device_id FROM vendor_firmware v
	JOIN board_model_aliases a ON a.code_norm = upper(btrim(regexp_replace(v.build, '[[:space:]_/.]+', '-', 'g'), '-'))`

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
		d = &Device{ID: id, Stock: []Firmware{}}
	}
	boards := []boardRef{}
	rows, err := a.DB.Query(ctx, `
		SELECT DISTINCT m.id, m.model FROM (`+BoardDevicesSQL+`) d JOIN board_models m ON m.id = d.model_id
		WHERE d.device_id = $1 ORDER BY m.id`, id)
	if err == nil {
		for rows.Next() {
			var b boardRef
			if err = rows.Scan(&b.ID, &b.Model); err != nil {
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
