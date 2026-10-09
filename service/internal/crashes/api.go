package crashes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/httpx"
	"github.com/OpenIPC/website/service/internal/reports"
	"github.com/OpenIPC/website/service/internal/snapshots"
)

// Limits on sending.
const (
	DailyPerClient = 20 // crashes per client address per day
	DailyPerCamera = 5  // per camera (MAC) per day
	maxMeta        = 64 << 10
	maxField       = 200
)

// API is the crashes' public addresses on the web role. The members' and
// the maintainers' are the club's (internal/club), under its cookie's path.
type API struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
	// SiteURL is where a sent crash's link points.
	SiteURL string
	Now     func() time.Time
}

func (a *API) Handlers() map[string]http.Handler {
	return map[string]http.Handler{
		"POST /api/v1/crashes":     http.HandlerFunc(a.upload),
		"GET /api/v1/crashes":      http.HandlerFunc(a.list),
		"GET /api/v1/crashes/{id}": http.HandlerFunc(a.get),
	}
}

// Store is the crashes' rows.
func (a *API) Store() *Store { return &Store{DB: a.DB} }

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// upload is POST /api/v1/crashes, from the camera's WebUI.
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	a.Submit(w, r, "", "webui")
}

// Submit is a crash sent: from a camera (member "") or from /club by a
// signed-in member. The body is multipart -- the part bundle (the WebUI's
// crashlog_*.tar.gz), and the fields mac, firmware, majestic, soc, sensor
// and meta (the firmware's meta.json) -- or the bundle alone.
func (a *API) Submit(w http.ResponseWriter, r *http.Request, member, channel string) {
	ctx := r.Context()
	key, err := (&reports.Store{DB: a.DB}).Key(ctx)
	if err != nil {
		a.fail(w, "the report key", err)
		return
	}
	in, status, err := read(r)
	if err != nil {
		a.refuse(w, status, err.Error())
		return
	}
	mac := strings.ToLower(strings.TrimSpace(in.fields["mac"]))
	macKey := ""
	if mac != "" {
		if !macRe.MatchString(mac) {
			a.refuse(w, http.StatusBadRequest, "mac: six pairs of hex digits, aa:bb:cc:dd:ee:ff")
			return
		}
		if macKey = snapshots.MACKey(mac); strings.Trim(macKey, "0") == "" || strings.Trim(macKey, "f") == "" {
			macKey = ""
		}
	}
	soc, sensor := strings.ToLower(in.fields["soc"]), strings.ToLower(in.fields["sensor"])
	for name, v := range map[string]string{"soc": soc, "sensor": sensor} {
		if v != "" && !hardwareRe.MatchString(v) {
			a.refuse(w, http.StatusBadRequest, name+": a chip's name as ipcinfo prints it, like gk7205v300 or imx335")
			return
		}
	}
	// A member names only a camera of theirs: the MAC is what pays a
	// crash's stars to the camera's owner.
	if member != "" && macKey != "" {
		mine, err := a.Store().LinkedTo(ctx, macKey, member)
		if err != nil {
			a.fail(w, "the camera", err)
			return
		}
		if !mine {
			a.refuse(w, http.StatusBadRequest, "mac: that camera is not linked to you on the Open Wall; link it from /club, or send the crash without it")
			return
		}
	}
	client := reports.Keyed(key, "client", httpx.ClientIP(r))

	files, err := Unpack(in.bundle)
	if err != nil {
		a.refuse(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	c, err := Parse(files)
	if err != nil {
		a.refuse(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	redactCrash(c, mac, key)
	// majestic's dump is kept as sent only until it is symbolized
	// (crash_dumps); what is kept for good is the dump without its stack.
	kept := in.bundle
	if c.Dump != nil {
		d, err := ReadDump(c.Dump)
		if err != nil {
			a.refuse(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		kept = d.stripped(c.Dump)
	}
	sum := sha256.Sum256(kept)
	e := &Event{
		ID: NewID(), Channel: channel, MACKey: macKey, Member: member, ClientKey: client,
		Firmware: in.fields["firmware"], Majestic: orDefault(in.fields["majestic"], c.Majestic),
		SoC: orDefault(soc, c.SoC), Sensor: orDefault(sensor, c.Sensor),
		Crash: c, Bundle: kept, BundleSHA256: hex.EncodeToString(sum[:]),
		SignatureID: c.Fatal.Signature(),
		Redacted:    Redact(c.Text, mac, key),
	}
	// What the firmware wrote about the camera when it kept the crash: the
	// meta field, or the meta.json in the bundle, which a bundle downloaded
	// from the WebUI and sent from /club carries. The log is the tail of the
	// kernel's; on a camera whose log filled up, the lines naming the chip and
	// the sensor are gone from it, and meta.json still names them.
	// Sent as the field, it must be JSON; found in the bundle, one that is not
	// is left out rather than costing the crash it came with.
	if in.meta == "" && len(files["meta.json"]) <= maxMeta {
		if m := strings.TrimSpace(files["meta.json"]); json.Valid([]byte(m)) {
			in.meta = m
		}
	}
	if in.meta != "" {
		red := Redact(in.meta, mac, key)
		if !json.Valid([]byte(red)) {
			a.refuse(w, http.StatusBadRequest, "meta is not JSON")
			return
		}
		e.Meta = json.RawMessage(red)
		var named struct{ SoC, Sensor string }
		if json.Unmarshal(e.Meta, &named) == nil {
			if v := strings.ToLower(named.SoC); e.SoC == "" && hardwareRe.MatchString(v) {
				e.SoC = v
			}
			if v := strings.ToLower(named.Sensor); e.Sensor == "" && hardwareRe.MatchString(v) {
				e.Sensor = v
			}
		}
	}
	id, sig, dup, err := a.Store().Insert(ctx, e, a.now())
	if errors.Is(err, ErrQuota) {
		w.Header().Set("Retry-After", "3600")
		a.refuse(w, http.StatusTooManyRequests, fmt.Sprintf("%d crashes a day from one address, %d from one camera, is the limit", DailyPerClient, DailyPerCamera))
		return
	}
	if err != nil {
		a.fail(w, "the crash", err)
		return
	}
	a.Log.Info("crashes: received", "id", id, "signature", sig, "kind", c.Kind, "duplicate", dup, "channel", channel,
		"soc", e.SoC, "self_inflicted", c.SelfInflicted)
	code := http.StatusCreated
	if dup {
		code = http.StatusOK
	}
	// majestic's crashes are the maintainers': the public list never has
	// them, and the sender is told the signal, not where in majestic.
	link := strings.TrimRight(a.SiteURL, "/") + "/crashes/#" + sig
	title := c.Fatal.Title()
	if Class(c.Kind) == "user" {
		link = strings.TrimRight(a.SiteURL, "/") + "/club/crashes/"
		title = c.Fatal.Reason
	}
	writeJSON(w, code, map[string]any{
		"id": id, "signature": sig, "title": title, "kind": c.Kind, "duplicate": dup,
		"self_inflicted": c.SelfInflicted,
		"url":            link,
	})
}

var (
	macRe      = regexp.MustCompile(`^[0-9a-f]{2}([:-]?[0-9a-f]{2}){5}$`)
	hardwareRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,39}$`)
)

// redactCrash replaces identifiers in every part of a parsed crash that is
// stored apart from the log -- the command line, the lines before it, each
// trace's reason (a panic's message is free text) -- and drops a SoC or
// sensor the log names in any shape but a chip's. The title and the
// signature are made from what is left.
func redactCrash(c *Crash, mac, key string) {
	c.Cmdline = Redact(c.Cmdline, mac, key)
	for i := range c.Leadup {
		c.Leadup[i] = Redact(c.Leadup[i], mac, key)
	}
	for _, t := range append([]*Trace{c.Fatal}, c.Before...) {
		t.Reason = Redact(t.Reason, mac, key)
	}
	if !hardwareRe.MatchString(c.SoC) {
		c.SoC = ""
	}
	if !hardwareRe.MatchString(c.Sensor) {
		c.Sensor = ""
	}
}

type upload struct {
	bundle []byte
	meta   string
	fields map[string]string
}

func read(r *http.Request) (*upload, int, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, MaxBundle+maxMeta+64<<10)
	out := &upload{fields: map[string]string{}}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "multipart/form-data" {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("the bundle is larger than %d bytes", MaxBundle)
		}
		out.bundle = b
		return out, 0, nil
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("the bundle is larger than %d bytes", MaxBundle)
			}
			return nil, http.StatusBadRequest, err
		}
		name := p.FormName()
		switch name {
		case "bundle":
			b, err := io.ReadAll(io.LimitReader(p, MaxBundle+1))
			if err != nil {
				return nil, http.StatusBadRequest, err
			}
			if len(b) > MaxBundle {
				return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("the bundle is larger than %d bytes", MaxBundle)
			}
			out.bundle = b
		case "meta":
			b, err := io.ReadAll(io.LimitReader(p, maxMeta+1))
			if err != nil {
				return nil, http.StatusBadRequest, err
			}
			if len(b) > maxMeta {
				return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("meta is larger than %d bytes", maxMeta)
			}
			out.meta = strings.TrimSpace(string(b))
		case "mac", "firmware", "majestic", "soc", "sensor":
			b, err := io.ReadAll(io.LimitReader(p, maxField+1))
			if err != nil {
				return nil, http.StatusBadRequest, err
			}
			if len(b) > maxField {
				return nil, http.StatusBadRequest, fmt.Errorf("%s: at most %d characters", name, maxField)
			}
			out.fields[name] = strings.TrimSpace(string(b))
		}
	}
	if len(out.bundle) == 0 {
		return nil, http.StatusBadRequest, errors.New("no bundle: send the crash log as the part bundle")
	}
	return out, 0, nil
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// Redact replaces the camera's MAC (and any other, in any of its spellings)
// and every IP address in a crash's text with a keyed hash: two crashes of
// one camera still show they are one camera's, and neither says which.
func Redact(text, mac, key string) string {
	text = reports.Redact(text, reports.Facts{MAC: mac}, key)
	text = replace(text, dottedMAC, func(m string) string { return "<mac:" + reports.Keyed(key, "mac", m) + ">" },
		func(prev, next byte) bool { return !hexOrDot(prev) && !hexOrDot(next) })
	text = replace(text, ipv4, func(m string) string {
		if m == "0.0.0.0" || strings.HasPrefix(m, "127.") || strings.HasPrefix(m, "255.") {
			return m
		}
		return "<ip:" + reports.Keyed(key, "ip", m) + ">"
	}, func(prev, next byte) bool { return !digitOrDot(prev) && !digitOrDot(next) })
	return replace(text, ipv6, func(m string) string {
		if m == "::" || m == "::1" {
			return m
		}
		return "<ip:" + reports.Keyed(key, "ip", strings.ToLower(m)) + ">"
	}, func(prev, next byte) bool { return !hexOrColon(prev) && !hexOrColon(next) })
}

// replace rewrites each match of re whose neighbours ok accepts. The
// neighbours are looked at, never matched, so two addresses one separator
// apart -- ip=10.0.0.2:10.0.0.1 -- are both found.
func replace(text string, re *regexp.Regexp, with func(string) string, ok func(prev, next byte) bool) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringIndex(text, -1) {
		var prev, next byte
		if m[0] > 0 {
			prev = text[m[0]-1]
		}
		if m[1] < len(text) {
			next = text[m[1]]
		}
		if !ok(prev, next) {
			continue
		}
		b.WriteString(text[last:m[0]])
		b.WriteString(with(text[m[0]:m[1]]))
		last = m[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

func digitOrDot(c byte) bool { return c == '.' || c >= '0' && c <= '9' }
func hexOrDot(c byte) bool   { return c == '.' || isHex(c) }
func hexOrColon(c byte) bool {
	return c == ':' || isHex(c)
}
func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

var (
	// ipv4 is a dotted quad; replace refuses one inside a longer dotted
	// number (a version, 4.9.37.1.2).
	ipv4 = regexp.MustCompile(`(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}`)
	// ipv6: eight groups, or fewer with "::". Never a time (17:57:19) or a
	// register dump (9dc0:), which have neither.
	ipv6 = regexp.MustCompile(`(?i)(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}|(?:[0-9a-f]{1,4}:){1,7}:(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){0,6})?|::(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){0,6})?`)
	// dottedMAC is Cisco's 0012.3456.789a.
	dottedMAC = regexp.MustCompile(`(?i)[0-9a-f]{4}\.[0-9a-f]{4}\.[0-9a-f]{4}`)
)

// list is GET /api/v1/crashes: the signatures, worst first. No log, no
// camera, no member.
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store().Ranked(r.Context(), false, a.now())
	if err != nil {
		a.fail(w, "the crashes", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSONCached(w, http.StatusOK, map[string]any{"signatures": orEmpty(list)})
}

// get is GET /api/v1/crashes/{id}: a signature and where it was seen.
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g, err := a.Store().Get(r.Context(), id, false, a.now())
	if errors.Is(err, ErrNoSignature) {
		a.refuse(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		a.fail(w, "the crash", err)
		return
	}
	combos, err := a.Store().Combos(r.Context(), id, false)
	if err != nil {
		a.fail(w, "the crash", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSONCached(w, http.StatusOK, map[string]any{"signature": g, "seen_on": orEmpty(combos)})
}

func (a *API) refuse(w http.ResponseWriter, status int, reason string) {
	a.Log.Info("crashes: refused", "status", status, "reason", reason)
	writeJSON(w, status, map[string]string{"error": reason})
}

func (a *API) fail(w http.ResponseWriter, what string, err error) {
	a.Log.Error("crashes: "+what+" failed", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the crash could not be stored; retry"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSONCached(w, status, v)
}

func writeJSONCached(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
