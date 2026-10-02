package reports

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
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/OpenIPC/website/service/internal/httpx"
)

// Limits on one upload.
const (
	DailyPerClient = 10 // reports per client address per day
	maxFiles       = 24
	maxPhoto       = 20 << 20
	maxText        = 4 << 20
	maxDocument    = 20 << 20
	maxField       = 4000
)

// API is the reports' addresses on the web role.
type API struct {
	DB    *pgxpool.Pool
	Files *Files
	// AccelPrefix is nginx's internal location aliasing Files.Root.
	AccelPrefix string
	Log         *slog.Logger
	Now         func() time.Time
}

func (a *API) Handlers() map[string]http.Handler {
	return map[string]http.Handler{
		"POST /api/v1/reports":                      http.HandlerFunc(a.upload),
		"GET /api/v1/reports":                       http.HandlerFunc(a.forModel),
		"GET /api/v1/reports/{id}":                  http.HandlerFunc(a.view),
		"GET /api/v1/reports/{id}/files/{position}": http.HandlerFunc(a.file),
		"POST /api/v1/boards/identify":              http.HandlerFunc(a.identify),
	}
}

func (a *API) store() *Store { return &Store{DB: a.DB} }

// Store is the reports' rows, for the club's pages.
func (a *API) Store() *Store { return a.store() }

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// part is one received file before it is kept.
type part struct {
	kind, name, mime string
	in               *Incoming
}

// received is an upload read off the wire.
type received struct {
	yaml    string
	backup  *Incoming
	parts   []part
	fields  map[string]string
	discard func()
}

var textKinds = map[string]bool{"boot_log": true, "uboot_env": true, "note": true}
var fileKinds = map[string]int64{"photo": maxPhoto, "boot_log": maxText, "uboot_env": maxText, "note": maxText, "document": maxDocument}

// upload is POST /api/v1/reports. The body is multipart -- a yaml part (or
// field), an optional backup, any of photo, boot_log, uboot_env, note and
// document, and the fields consent, channel, tool and note -- or, for
// `ipctool | curl --data-binary @- .../api/v1/reports`, ipctool's output as
// the whole body.
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	a.Submit(w, r, "")
}

// Submit is an upload, from ipctool or from the site's send form
// (POST /api/v1/club/reports, member set when the sender is signed in).
// A send that names a catalogue board (the field model) needs no ipctool
// output: a boot log or a photo of a known board is a report too, and so is
// a flash dump read with a programmer rather than ipctool.
func (a *API) Submit(w http.ResponseWriter, r *http.Request, member string) {
	ctx := r.Context()
	st := a.store()
	key, err := st.Key(ctx)
	if err != nil {
		a.fail(w, "the report key", err)
		return
	}
	client := Keyed(key, "client", httpx.ClientIP(r))
	n, err := st.UploadsSince(ctx, client, a.now().Add(-24*time.Hour))
	if err != nil {
		a.fail(w, "the daily count", err)
		return
	}
	if n >= DailyPerClient {
		w.Header().Set("Retry-After", "3600")
		a.refuse(w, http.StatusTooManyRequests, fmt.Sprintf("%d reports a day from one address is the limit; send the rest tomorrow", DailyPerClient))
		return
	}

	in, status, err := a.read(r)
	if in != nil {
		defer in.discard()
	}
	if err != nil {
		a.refuse(w, status, err.Error())
		return
	}
	if len(in.fields["tool"]) > 200 {
		a.refuse(w, http.StatusBadRequest, "tool: at most 200 characters, a name and a version")
		return
	}
	channel := orDefault(in.fields["channel"], "ipctool")
	if channel != "ipctool" && channel != "agent" && channel != "web" {
		a.refuse(w, http.StatusBadRequest, "channel is ipctool, agent or web")
		return
	}

	model := in.fields["model"]
	if model != "" {
		if channel != "web" {
			a.refuse(w, http.StatusBadRequest, "model is the send form's field: channel web")
			return
		}
		known, err := st.ModelExists(ctx, model)
		if err != nil {
			a.fail(w, "the board", err)
			return
		}
		if !known {
			a.refuse(w, http.StatusBadRequest, "model: the catalogue has no board "+model)
			return
		}
	}

	// A backup carries the YAML it was taken with; alone, it is the report.
	// For a named board, a whole flash image read with a programmer is one
	// too: the bytes as they are, a power of two from 1 MB.
	var backup Backup
	raw := false
	if in.backup != nil {
		f, err := in.backup.Open()
		if err == nil {
			backup, err = ReadBackup(f)
		}
		if err != nil && model != "" && flashImage(in.backup.Bytes) {
			raw, err = true, nil
		}
		if err != nil {
			a.refuse(w, http.StatusBadRequest, "backup: "+err.Error())
			return
		}
		if raw {
			// the image says nothing about itself; the board is the sender's word
		} else if in.yaml == "" {
			in.yaml = backup.YAML
		} else if Clean(in.yaml) != Clean(backup.YAML) {
			a.refuse(w, http.StatusBadRequest, "the backup was taken with other ipctool output than the yaml sent with it")
			return
		}
	}
	var doc string
	var facts Facts
	if in.yaml != "" || model == "" {
		doc, facts, err = Parse(in.yaml)
		if err != nil {
			a.refuse(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if doc == "" && in.backup == nil && len(in.parts) == 0 && in.fields["note"] == "" {
		a.refuse(w, http.StatusBadRequest, "nothing to send: add a file, a photo or a note")
		return
	}
	consent := "none"
	if in.backup != nil {
		consent = orDefault(in.fields["consent"], "private")
		if consent != "private" && consent != "public" {
			a.refuse(w, http.StatusBadRequest, "consent is private (OpenIPC's maintainers only) or public (published with the report)")
			return
		}
	}

	rep := &Report{
		Channel: channel, Tool: in.fields["tool"], Note: in.fields["note"],
		NotePublic: Redact(in.fields["note"], facts, key),
		YAML:       doc, YAMLPublic: Redact(doc, facts, key), Facts: facts,
		IDHashes: facts.IDHashes(key), Consent: consent, ClientHash: client,
		Member: member, Model: model,
	}
	sum := sha256.Sum256([]byte(doc))
	rep.YAMLSHA256 = hex.EncodeToString(sum[:])

	// Every file's name (its sha256) is known before anything is placed, so
	// the insert can lock them: the files go into the store inside the
	// transaction that names them, and a takedown removing the same bytes
	// waits for it (Store.Insert, Store.RemoveUnreferenced).
	var places []func() error
	prepare := func(p part) (File, error) {
		f := File{Kind: p.kind, Name: p.name, Mime: p.mime, Bytes: p.in.Bytes, SHA256: p.in.Sum()}
		inc := p.in
		places = append(places, func() error { _, err := a.Files.Keep(inc); return err })
		switch {
		case p.kind == "backup" && consent != "public":
			// stored for the maintainers, never served
		case textKinds[p.kind]:
			rd, err := p.in.Open()
			if err != nil {
				return f, err
			}
			text, err := io.ReadAll(rd)
			if err != nil {
				return f, err
			}
			pub := []byte(Redact(string(text), facts, key))
			ps := sha256.Sum256(pub)
			f.PublicSHA256, f.PublicBytes = hex.EncodeToString(ps[:]), int64(len(pub))
			places = append(places, func() error { _, err := a.Files.Put(pub); return err })
		default:
			f.PublicSHA256, f.PublicBytes = f.SHA256, f.Bytes
		}
		return f, nil
	}
	all := in.parts
	if in.backup != nil {
		name := "backup.bin"
		if raw {
			name = "flash.bin"
		}
		all = append([]part{{kind: "backup", name: name, mime: "application/octet-stream", in: in.backup}}, all...)
	}
	for _, p := range all {
		f, err := prepare(p)
		if err != nil {
			a.fail(w, "a file", err)
			return
		}
		rep.Files = append(rep.Files, f)
	}
	err = st.Insert(ctx, rep, DailyPerClient, func() error {
		for _, place := range places {
			if err := place(); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, ErrQuota) {
		w.Header().Set("Retry-After", "3600")
		a.refuse(w, http.StatusTooManyRequests, fmt.Sprintf("%d reports a day from one address is the limit; send the rest tomorrow", DailyPerClient))
		return
	}
	if err != nil {
		a.fail(w, "the report", err)
		return
	}
	ident, err := Identify(ctx, a.DB, facts)
	if err != nil {
		a.Log.Warn("reports: identify failed", "report", rep.ID, "err", err)
	}
	a.Log.Info("reports: received", "report", rep.ID, "channel", channel, "chip", facts.ChipModel,
		"files", len(rep.Files), "consent", consent, "known", ident.Known)

	type receivedFile struct {
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Bytes   int64  `json:"bytes"`
		SHA256  string `json:"sha256"`
		Private bool   `json:"private,omitempty"`
	}
	files := []receivedFile{}
	for _, f := range rep.Files {
		files = append(files, receivedFile{f.Kind, f.Name, f.Bytes, f.SHA256, f.PublicSHA256 == ""})
	}
	out := map[string]any{
		"id": rep.ID, "status": "pending", "receipt_url": receiptURL(r, rep.ID),
		"received_at": rep.ReceivedAt, "backup_consent": consent,
		"facts": facts, "files": files, "identify": ident,
		"next": "OpenIPC's maintainers review each report before it is published. Nothing identifying the camera " +
			"(MAC, die ID, cloud ID) is ever shown; the receipt shows the report's state.",
	}
	if raw {
		out["backup"] = map[string]any{"partitions": 0, "flash_bytes": in.backup.Bytes}
	} else if in.backup != nil {
		out["backup"] = map[string]any{"partitions": len(backup.Blocks), "flash_bytes": backup.Size()}
	}
	if member != "" {
		out["receipt_url"] = clubURL(r)
	}
	writeJSON(w, http.StatusCreated, out)
}

// flashImage: the size of a whole NOR or NAND chip, 1 to 256 MB.
func flashImage(n int64) bool {
	return n >= 1<<20 && n <= MaxBackup && n&(n-1) == 0
}

func clubURL(r *http.Request) string {
	host := r.Host
	if host == "" {
		host = "openipc.org"
	}
	return "https://" + host + "/club/"
}

func receiptURL(r *http.Request, id string) string {
	host := r.Host
	if host == "" {
		host = "openipc.org"
	}
	return "https://" + host + "/cameras/report/?id=" + id
}

var safeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func fileName(given, kind string, i int) string {
	n := safeName.ReplaceAllString(path.Base(strings.ReplaceAll(given, `\`, "/")), "_")
	n = strings.Trim(n, "._-")
	if len(n) > 120 {
		n = n[len(n)-120:]
	}
	if n == "" || n == "." {
		n = kind + "-" + strconv.Itoa(i)
	}
	return n
}

// read takes the body apart, streaming every file to .incoming/ as it
// arrives, so a 128 MB backup never sits in memory.
func (a *API) read(r *http.Request) (*received, int, error) {
	in := &received{fields: map[string]string{}}
	in.discard = func() {
		in.backup.Discard()
		for _, p := range in.parts {
			p.in.Discard()
		}
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "multipart/form-data" {
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxYAML+1))
		if err != nil {
			return in, http.StatusBadRequest, errors.New("the body could not be read")
		}
		if len(body) > MaxYAML {
			return in, http.StatusRequestEntityTooLarge, fmt.Errorf("ipctool's output is larger than %d KB", MaxYAML>>10)
		}
		in.yaml = string(body)
		for _, k := range []string{"channel", "tool", "note"} {
			in.fields[k] = r.URL.Query().Get(k)
		}
		return in, 0, nil
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return in, http.StatusBadRequest, errors.New("the multipart body could not be read")
	}
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return in, http.StatusBadRequest, errors.New("the multipart body is cut short")
		}
		name := p.FormName()
		// "note" is both a field (a line of text) and a file kind (a note
		// file): a part with a filename is a file, one without is a field.
		isField := p.FileName() == "" && (name == "consent" || name == "channel" || name == "tool" || name == "note" || name == "model")
		switch {
		case isField:
			b, err := io.ReadAll(io.LimitReader(p, maxField+1))
			if err != nil || len(b) > maxField || !utf8.Valid(b) {
				return in, http.StatusBadRequest, fmt.Errorf("%s: at most %d characters of text", name, maxField)
			}
			in.fields[name] = strings.TrimSpace(string(b))
		case name == "yaml":
			b, err := io.ReadAll(io.LimitReader(p, MaxYAML+1))
			if err != nil || len(b) > MaxYAML {
				return in, http.StatusRequestEntityTooLarge, fmt.Errorf("yaml: larger than %d KB", MaxYAML>>10)
			}
			in.yaml = string(b)
		case name == "backup":
			if in.backup != nil {
				return in, http.StatusBadRequest, errors.New("one backup per report")
			}
			inc, err := a.Files.Receive(p, MaxBackup)
			if err != nil {
				return in, sizeStatus(err), fmt.Errorf("backup: %v", err)
			}
			in.backup = inc
		case fileKinds[name] > 0:
			if len(in.parts) == maxFiles {
				return in, http.StatusBadRequest, fmt.Errorf("at most %d files per report", maxFiles)
			}
			inc, err := a.Files.Receive(p, fileKinds[name])
			if err != nil {
				return in, sizeStatus(err), fmt.Errorf("%s: %v", name, err)
			}
			pt := part{kind: name, name: fileName(p.FileName(), name, len(in.parts)+1), in: inc}
			in.parts = append(in.parts, pt)
			mt, err := sniff(inc, name)
			if err != nil {
				return in, http.StatusUnsupportedMediaType, fmt.Errorf("%s %s: %v", name, pt.name, err)
			}
			in.parts[len(in.parts)-1].mime = mt
		default:
			return in, http.StatusBadRequest, fmt.Errorf("%q is not a part a report has: yaml, backup, photo, boot_log, uboot_env, note, document, consent, channel, tool, model", name)
		}
	}
	if in.yaml == "" && in.backup == nil && in.fields["model"] == "" {
		return in, http.StatusBadRequest, errors.New("a report needs ipctool's output: a yaml part, or a backup")
	}
	return in, 0, nil
}

func sizeStatus(err error) int {
	var big ErrTooLarge
	if errors.As(err, &big) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// sniff decides a file's type from its bytes, never from what the client
// declared: photos are JPEG, PNG or WebP; text is text; a document is a PDF.
func sniff(in *Incoming, kind string) (string, error) {
	rd, err := in.Open()
	if err != nil {
		return "", err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(rd, head)
	head = head[:n]
	got := http.DetectContentType(head)
	switch kind {
	case "photo":
		switch got {
		case "image/jpeg", "image/png", "image/webp":
			return got, nil
		}
		return "", errors.New("a photo is JPEG, PNG or WebP")
	case "document":
		if got == "application/pdf" {
			return got, nil
		}
		return "", errors.New("a document is a PDF")
	default:
		if strings.HasPrefix(got, "text/plain") || got == "application/octet-stream" && printable(head) {
			return "text/plain; charset=utf-8", nil
		}
		return "", errors.New("a console capture is text")
	}
}

// printable: a console capture with a stray control byte is still text.
func printable(b []byte) bool {
	bad := 0
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20 && c != 0x1b) {
			bad++
		}
	}
	return bad*20 < len(b)+1
}

// view is GET /api/v1/reports/{id}: the receipt, and once published the
// report.
func (a *API) view(w http.ResponseWriter, r *http.Request) {
	v, err := a.store().Public(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		a.refuse(w, http.StatusNotFound, "no report has this id")
		return
	}
	if err != nil {
		a.fail(w, "the report", err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// forModel is GET /api/v1/reports?model=<board model id>: the board's
// published reports, for its panel.
func (a *API) forModel(w http.ResponseWriter, r *http.Request) {
	model := r.URL.Query().Get("model")
	if model == "" || len(model) > 200 {
		a.refuse(w, http.StatusBadRequest, "?model= names a board of the catalogue")
		return
	}
	vs, err := a.store().ForModel(r.Context(), model)
	if err != nil {
		a.fail(w, "the board's reports", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	_ = json.NewEncoder(w).Encode(map[string]any{"schema": 1, "model": model, "reports": vs})
}

// file is GET /api/v1/reports/{id}/files/{position}: a published report's
// file, handed to nginx to send. A private backup, or any file of an
// unpublished report, is a 404 -- the same as one that does not exist.
func (a *API) file(w http.ResponseWriter, r *http.Request) {
	pos, err := strconv.Atoi(r.PathValue("position"))
	if err != nil || pos < 1 {
		http.NotFound(w, r)
		return
	}
	sum, name, mt, kind, err := a.store().Served(r.Context(), r.PathValue("id"), pos)
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, "the file", err)
		return
	}
	a.send(w, r.PathValue("id"), sum, name, mt, kind, "public, max-age=86400")
}

// ServeStored is any file of a report -- a private backup too, as it was
// sent -- for its sender or a maintainer (internal/club decides who). The
// caller has checked; this only hands the file to nginx.
func (a *API) ServeStored(w http.ResponseWriter, r *http.Request, id string, position int) {
	sum, name, mt, kind, err := a.store().StoredFile(r.Context(), id, position)
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

func (a *API) send(w http.ResponseWriter, id, sum, name, mt, kind, cache string) {
	disposition := "inline"
	if kind == "backup" || kind == "document" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", mt)
	w.Header().Set("Content-Disposition", disposition+`; filename="`+id+"-"+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", cache)
	w.Header().Set("X-Accel-Redirect", strings.TrimSuffix(a.AccelPrefix, "/")+"/"+Rel(sum))
	w.WriteHeader(http.StatusOK)
}

// identify is POST /api/v1/boards/identify: which catalogue boards
// ipctool's output may be from, before anything is uploaded. Nothing is
// stored.
func (a *API) identify(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxYAML+1))
	if err != nil || len(body) > MaxYAML {
		a.refuse(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("send ipctool's output, at most %d KB", MaxYAML>>10))
		return
	}
	_, facts, err := Parse(string(body))
	if err != nil {
		a.refuse(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := Identify(r.Context(), a.DB, facts)
	if err != nil {
		a.fail(w, "identify", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"facts": facts, "identify": id})
}

func (a *API) refuse(w http.ResponseWriter, status int, reason string) {
	a.Log.Info("reports: refused", "status", status, "reason", reason)
	writeJSON(w, status, map[string]string{"error": reason})
}

func (a *API) fail(w http.ResponseWriter, what string, err error) {
	a.Log.Error("reports: "+what+" failed", "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "the report could not be stored; retry"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
