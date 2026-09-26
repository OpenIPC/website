package snapshots

import (
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/OpenIPC/website/service/internal/httpx"
)

// Interval rules. A camera may send one frame per fifteen minutes, with two
// minutes of hysteresis because a quarter-hour cron drifts: the gate opens at
// 780 seconds. Retry-After is fifteen minutes less the elapsed time and does
// NOT subtract the hysteresis, so it over-reports by 120 s. That is the Rails
// behaviour, pinned by the conformance suite as a defect, and cameras in the
// field may read it; changing it is a separate, deliberate decision.
const (
	IntervalSeconds   = 900
	HysteresisSeconds = 120
)

// attributes are the fields the upload stores besides the MAC and the file.
var attributes = []string{"caption", "firmware", "flash_size", "hostname", "sensor", "soc",
	"soc_temperature", "streamer", "uptime"}

// Wall is where originals go.
type Wall interface {
	WriteOriginal(id string, data []byte) error
	Purge(id string) error
}

// Upload handles POST /snapshots, /ru/snapshots and /zh/snapshots -- the one
// request on the site whose clients cannot be updated. Everything it answers
// is frozen: see test/conformance/upload_contract_test.rb.
type UploadHandler struct {
	Store     *Store
	Wall      Wall
	Enqueue   func(publicID string)
	Blacklist []string
	Whitelist []string
	Log       *slog.Logger
	// Shadow marks a process receiving nginx's mirror of production uploads:
	// it decides and stores exactly as the real one would, into its own
	// database and wall root, and logs each decision against the request id
	// so the two can be compared.
	Shadow bool
}

func (h *UploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	locale, fromPath := httpx.Locale(r, r.PathValue("locale"))
	if !fromPath {
		httpx.VaryByAcceptLanguage(w.Header())
	}
	status := h.serve(w, r, locale)
	if h.Shadow {
		h.Log.Info("upload_decision", "request_id", r.Header.Get("X-Request-Id"), "status", status,
			"location", w.Header().Get("Location"))
	}
}

func (h *UploadHandler) serve(w http.ResponseWriter, r *http.Request, locale string) int {
	u, err := read(r)
	if err != nil {
		h.Log.Warn("upload: unreadable request", "err", err)
	}
	u.RemoteIP = httpx.ClientIP(r)

	errs := Errors(u, locale)

	mac := ""
	if u.MAC != nil {
		mac = *u.MAC
	}
	// The blacklist is checked after the other validations have run, and it
	// wins over all of them: a bare 403, no X-Error, even with no file.
	if u.MAC != nil && contains(h.Blacklist, mac) {
		httpx.Empty(w, http.StatusForbidden)
		return http.StatusForbidden
	}
	// So is the interval, and it wins over a file error too: Rails raised it
	// from inside validation, after collecting the rest. With nothing else
	// wrong, the check is made again under the camera's lock as the row is
	// inserted (Store.InsertIfDue), so two frames at once cannot both pass.
	exempt := contains(h.Whitelist, u.RemoteIP)
	if u.MAC != nil && !exempt && len(errs) > 0 {
		elapsed, found, err := h.Store.SecondsSinceLast(r.Context(), mac)
		if err != nil {
			return h.fail(w, "interval lookup failed", err)
		}
		if found && elapsed < IntervalSeconds-HysteresisSeconds {
			return tooSoon(w, elapsed)
		}
	}
	if len(errs) > 0 {
		w.Header().Set("X-Error", strings.Join(errs, ". "))
		httpx.Empty(w, http.StatusUnsupportedMediaType)
		return http.StatusUnsupportedMediaType
	}

	minElapsed := float64(IntervalSeconds - HysteresisSeconds)
	if exempt {
		minElapsed = 0
	}
	id, err := h.create(r, u, mac, minElapsed)
	var soon ErrTooSoon
	if errors.As(err, &soon) {
		return tooSoon(w, soon.Elapsed)
	}
	if err != nil {
		return h.fail(w, "not stored", err)
	}
	h.Enqueue(id)

	// A path, never a URL, and never a row id. The locale prefix follows the
	// language the request was answered in, as Rails' snapshot_path did.
	location := "/snapshots/" + id
	if locale != "en" {
		location = "/" + locale + location
	}
	w.Header().Set("Location", location)
	httpx.Empty(w, http.StatusCreated)
	return http.StatusCreated
}

// tooSoon is the 429: fifteen minutes less the elapsed time, whole seconds.
func tooSoon(w http.ResponseWriter, elapsed float64) int {
	w.Header().Set("Retry-After", strconv.Itoa(IntervalSeconds-int(elapsed)))
	httpx.Empty(w, http.StatusTooManyRequests)
	return http.StatusTooManyRequests
}

func (h *UploadHandler) fail(w http.ResponseWriter, what string, err error) int {
	h.Log.Error("upload: "+what, "err", err)
	httpx.Empty(w, http.StatusInternalServerError)
	return http.StatusInternalServerError
}

func (h *UploadHandler) create(r *http.Request, u *Upload, mac string, minElapsed float64) (string, error) {
	contentType := ContentType(u.File, u.Declared, u.Filename)
	for attempt := 0; ; attempt++ {
		id := NewPublicID()
		if err := h.Wall.WriteOriginal(id, u.File); err != nil {
			return "", err
		}
		err := h.Store.InsertIfDue(r.Context(), NewRow{
			PublicID: id, MAC: mac, IP: u.RemoteIP, Attributes: u.Attributes,
			ContentType: contentType, ByteSize: int64(len(u.File)),
		}, minElapsed)
		if err == nil {
			return id, nil
		}
		_ = h.Wall.Purge(id)
		if !IsUniqueViolation(err) || attempt >= 3 {
			return "", err
		}
	}
}

// read takes the multipart form a camera sends (curl -F), or an urlencoded
// one. Body fields win over the query string, as they did in Rails' params.
// The body is already capped at 1 MB by nginx; the cap here is for a request
// that reaches the process some other way.
func read(r *http.Request) (*Upload, error) {
	u := &Upload{Attributes: map[string]*string{}}
	r.Body = http.MaxBytesReader(nil, r.Body, 8<<20)
	err := r.ParseMultipartForm(8 << 20)
	if errors.Is(err, http.ErrNotMultipart) {
		err = r.ParseForm()
	}
	field := func(name string) *string {
		if r.MultipartForm != nil {
			if v, ok := r.MultipartForm.Value[name]; ok && len(v) > 0 {
				return &v[0]
			}
		}
		if v, ok := r.PostForm[name]; ok && len(v) > 0 {
			return &v[0]
		}
		if v, ok := r.URL.Query()[name]; ok && len(v) > 0 {
			return &v[0]
		}
		return nil
	}
	u.MAC = field("mac_address")
	for _, name := range attributes {
		u.Attributes[name] = field(name)
	}
	if r.MultipartForm != nil {
		if files := r.MultipartForm.File["file"]; len(files) > 0 {
			if data, derr := readPart(files[0]); derr == nil {
				u.File, u.HasFile = data, true
				u.Filename = files[0].Filename
				u.Declared = files[0].Header.Get("Content-Type")
			} else if err == nil {
				err = derr
			}
		}
	}
	return u, err
}

func readPart(fh *multipart.FileHeader) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
