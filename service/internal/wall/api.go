package wall

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/OpenIPC/website/service/internal/httpx"
	"github.com/OpenIPC/website/service/internal/snapshots"
)

// The wall's numbers. Each address authorises exactly what its page draws:
// this many tiles, at these variants, and no more.
const (
	MosaicTiles = 5
	PerPage     = 18
	StripEager  = 12
)

var (
	numeric     = regexp.MustCompile(`^[0-9]+$`)
	wallID      = regexp.MustCompile(`^(?:[0-9a-f]{20}|[0-9]+)$`)
	cameraToken = regexp.MustCompile(`^[0-9a-f]{16}$`)
	pageNumber  = regexp.MustCompile(`^[0-9]+$`)
)

// Snapshots is what the API reads.
type Snapshots interface {
	LatestPerCamera(ctx context.Context, limit int) ([]*snapshots.Snapshot, error)
	ByPublicID(ctx context.Context, id string) (*snapshots.Snapshot, error)
	ByCameraToken(ctx context.Context, token string) (*snapshots.Snapshot, error)
	DayOf(ctx context.Context, subject *snapshots.Snapshot, limit int) ([]*snapshots.Snapshot, error)
}

// API serves /api/v1/wall/*.json, never the socket.
type API struct {
	Store   Snapshots
	Granter *Granter
	Log     *slog.Logger
}

// Routes registers the five addresses on mux. The path segment carries the
// extension (Go's patterns match whole segments), so each handler checks and
// strips it; anything that does not fit answers 404.
func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/wall/mosaic.json", a.mosaic)
	mux.HandleFunc("GET /api/v1/wall/page/{page}", a.page)
	mux.HandleFunc("GET /api/v1/wall/snapshot/{file}", a.snapshot)
	mux.HandleFunc("GET /api/v1/wall/snapshot/{id}/{file}", a.snapshotDay)
	mux.HandleFunc("GET /api/v1/wall/camera/{file}", a.camera)
}

func jsonStem(file string) (string, bool) {
	if len(file) > 5 && file[len(file)-5:] == ".json" {
		return file[:len(file)-5], true
	}
	return "", false
}

type tile struct {
	ID     string  `json:"id"`
	SoC    *string `json:"soc"`
	Sensor *string `json:"sensor"`
}

type card struct {
	ID             string  `json:"id"`
	SoC            *string `json:"soc"`
	Sensor         *string `json:"sensor"`
	Firmware       *string `json:"firmware"`
	Streamer       *string `json:"streamer"`
	Uptime         *string `json:"uptime"`
	SoCTemperature *string `json:"soc_temperature"`
	Dimensions     string  `json:"dimensions"`
	Bytes          int64   `json:"bytes"`
	At             int64   `json:"at"`
}

type detail struct {
	card
	Caption *string `json:"caption"`
	Camera  string  `json:"camera"`
}

// MarshalJSON keeps detail's keys in a fixed order: the card's, then caption
// and camera. Embedding alone would do that too; this makes it explicit.
func (d detail) MarshalJSON() ([]byte, error) {
	c, err := json.Marshal(d.card)
	if err != nil {
		return nil, err
	}
	extra, err := json.Marshal(struct {
		Caption *string `json:"caption"`
		Camera  string  `json:"camera"`
	}{d.Caption, d.Camera})
	if err != nil {
		return nil, err
	}
	return append(append(c[:len(c)-1], ','), extra[1:]...), nil
}

type icon struct {
	ID string `json:"id"`
	At int64  `json:"at"`
}

func toTile(s *snapshots.Snapshot) tile {
	return tile{ID: s.PublicID, SoC: snapshots.Presence(s.SoC), Sensor: snapshots.Presence(s.Sensor)}
}

func toCard(s *snapshots.Snapshot) card {
	return card{
		ID: s.PublicID, SoC: snapshots.Presence(s.SoC), Sensor: snapshots.Presence(s.Sensor),
		Firmware: snapshots.Presence(s.Firmware), Streamer: snapshots.Presence(s.Streamer),
		Uptime: snapshots.Presence(s.Uptime), SoCTemperature: snapshots.Presence(s.SoCTemperature),
		Dimensions: dimensions(s), Bytes: s.ByteSize, At: s.CreatedAt.Unix(),
	}
}

// dimensions is [width, height].join('x'), which reads "x" before the image
// has been measured.
func dimensions(s *snapshots.Snapshot) string {
	w, h := "", ""
	if s.Width != nil {
		w = strconv.Itoa(int(*s.Width))
	}
	if s.Height != nil {
		h = strconv.Itoa(int(*s.Height))
	}
	return w + "x" + h
}

func toIcon(s *snapshots.Snapshot) icon { return icon{ID: s.PublicID, At: s.CreatedAt.Unix()} }

func pairs(rows []*snapshots.Snapshot, variant string) []string {
	out := make([]string, len(rows))
	for i, s := range rows {
		out[i] = Pair(s.PublicID, variant)
	}
	return out
}

// render answers with a minute of public freshness, readable from a
// mirror, and a body that is byte-stable within a grant bucket.
func (a *API) render(w http.ResponseWriter, r *http.Request, body any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(body); err != nil {
		a.fail(w, err)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "max-age=60, public")
	h.Set("Access-Control-Allow-Origin", "*")
	httpx.VaryByAcceptLanguage(h)
	httpx.WriteJSON(w, r, bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// status answers a bare status: no body, and no-cache so nothing stores a miss.
func status(w http.ResponseWriter, code int) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-cache")
	httpx.VaryByAcceptLanguage(h)
	httpx.Empty(w, code)
}

func (a *API) fail(w http.ResponseWriter, err error) {
	a.Log.Error("wall api", "err", err)
	status(w, http.StatusInternalServerError)
}

func (a *API) mosaic(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.LatestPerCamera(r.Context(), MosaicTiles)
	if err != nil {
		a.fail(w, err)
		return
	}
	tiles := make([]tile, len(rows))
	for i, s := range rows {
		tiles[i] = toTile(s)
	}
	a.render(w, r, struct {
		Variant string  `json:"variant"`
		Grant   *string `json:"grant"`
		Tiles   []tile  `json:"tiles"`
	}{"thumb", a.Granter.Issue(pairs(rows, "thumb")), tiles})
}

func (a *API) page(w http.ResponseWriter, r *http.Request) {
	stem, ok := jsonStem(r.PathValue("page"))
	if !ok || !pageNumber.MatchString(stem) {
		http.NotFound(w, r)
		return
	}
	number, err := strconv.Atoi(stem)
	if err != nil {
		http.NotFound(w, r) // more digits than an int holds: no such page
		return
	}
	if number < 1 {
		number = 1
	}
	all, err := a.Store.LatestPerCamera(r.Context(), 0)
	if err != nil {
		a.fail(w, err)
		return
	}
	// Compared before multiplying, so a page number near the top of an int
	// cannot overflow into a negative slice bound.
	var rows []*snapshots.Snapshot
	if number-1 < (len(all)+PerPage-1)/PerPage {
		from := (number - 1) * PerPage
		rows = all[from:min(from+PerPage, len(all))]
	}
	pages := max((len(all)+PerPage-1)/PerPage, 1)
	cards := make([]card, len(rows))
	for i, s := range rows {
		cards[i] = toCard(s)
	}
	a.render(w, r, struct {
		Variant string  `json:"variant"`
		Page    int     `json:"page"`
		Pages   int     `json:"pages"`
		Tiles   []card  `json:"tiles"`
		Grant   *string `json:"grant"`
	}{"thumb", number, pages, cards, a.Granter.Issue(pairs(rows, "thumb"))})
}

// find is find_snapshot: a number is gone (410), an unknown id is missing
// (404), and neither has a body.
func (a *API) find(w http.ResponseWriter, r *http.Request, id string) *snapshots.Snapshot {
	if numeric.MatchString(id) {
		status(w, http.StatusGone)
		return nil
	}
	s, err := a.Store.ByPublicID(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return nil
	}
	if s == nil {
		status(w, http.StatusNotFound)
	}
	return s
}

func (a *API) snapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := jsonStem(r.PathValue("file"))
	if !ok || !wallID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	if subject := a.find(w, r, id); subject != nil {
		a.renderSnapshot(w, r, subject)
	}
}

func (a *API) camera(w http.ResponseWriter, r *http.Request) {
	token, ok := jsonStem(r.PathValue("file"))
	if !ok || !cameraToken.MatchString(token) {
		http.NotFound(w, r)
		return
	}
	subject, err := a.Store.ByCameraToken(r.Context(), token)
	if err != nil {
		a.fail(w, err)
		return
	}
	if subject == nil {
		status(w, http.StatusNotFound)
		return
	}
	a.renderSnapshot(w, r, subject)
}

// renderSnapshot is the frame itself and the first screenful of its camera's
// day. STRIP_EAGER + 1 rows and no COUNT: the extra row is how strip_more is
// known.
func (a *API) renderSnapshot(w http.ResponseWriter, r *http.Request, subject *snapshots.Snapshot) {
	rows, err := a.Store.DayOf(r.Context(), subject, StripEager+1)
	if err != nil {
		a.fail(w, err)
		return
	}
	strip := rows[:min(len(rows), StripEager)]
	icons := make([]icon, len(strip))
	for i, s := range strip {
		icons[i] = toIcon(s)
	}
	a.render(w, r, struct {
		Variant      string  `json:"variant"`
		StripVariant string  `json:"strip_variant"`
		Snapshot     detail  `json:"snapshot"`
		Strip        []icon  `json:"strip"`
		StripMore    bool    `json:"strip_more"`
		Grant        *string `json:"grant"`
	}{"fullhd", "icon2",
		detail{card: toCard(subject), Caption: snapshots.Presence(subject.Caption), Camera: subject.CameraToken},
		icons, len(rows) > StripEager,
		a.Granter.Issue(append(pairs(strip, "icon2"), Pair(subject.PublicID, "fullhd")))})
}

// snapshotDay is archive.json (the day at icon2, newest first) and
// slideshow.json (the day at fullhd, oldest first).
func (a *API) snapshotDay(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stem, ok := jsonStem(r.PathValue("file"))
	if !ok || (stem != "archive" && stem != "slideshow") || !wallID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	subject := a.find(w, r, id)
	if subject == nil {
		return
	}
	rows, err := a.Store.DayOf(r.Context(), subject, 0)
	if err != nil {
		a.fail(w, err)
		return
	}
	variant := "icon2"
	if stem == "slideshow" {
		variant = "fullhd"
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	frames := make([]icon, len(rows))
	for i, s := range rows {
		frames[i] = toIcon(s)
	}
	a.render(w, r, struct {
		Variant string  `json:"variant"`
		Frames  []icon  `json:"frames"`
		Grant   *string `json:"grant"`
	}{variant, frames, a.Granter.Issue(pairs(rows, variant))})
}
