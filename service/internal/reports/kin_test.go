package reports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// seed is one report as the queue of 2026-10-23 held it, written straight
// into the tables: only what Kin compares matters here.
type seed struct {
	id     string
	ago    time.Duration
	yaml   string
	note   string
	ids    map[string]string
	client string
	files  []string // contents; the sha256 is what is compared
	member bool
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (e *env) seed(t *testing.T, rs ...seed) {
	t.Helper()
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `INSERT INTO club_members (id, name) VALUES ('m-kin0000000', 'Kin') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		ids, _ := json.Marshal(r.ids)
		if r.ids == nil {
			ids = []byte("{}")
		}
		if _, err := e.pool.Exec(ctx, `
			INSERT INTO reports (id, received_at, channel, note, note_public, yaml, yaml_public, yaml_sha256, id_hashes, backup_consent, client_hash)
			VALUES ($1, now() - make_interval(secs => $2), 'agent', $3, $3, $4, $4, $5, $6, 'private', $7)`,
			r.id, r.ago.Seconds(), r.note, r.yaml, sum(r.yaml), ids, r.client); err != nil {
			t.Fatal(err)
		}
		for i, f := range r.files {
			if _, err := e.pool.Exec(ctx, `
				INSERT INTO report_files (report_id, position, kind, name, mime, sha256, bytes)
				VALUES ($1, $2, 'photo', 'f.jpg', 'image/jpeg', $3, $4)`, r.id, i+1, sum(f), len(f)); err != nil {
				t.Fatal(err)
			}
		}
		if r.member {
			if _, err := e.pool.Exec(ctx, `INSERT INTO report_submissions (report_id, member_id) VALUES ($1, 'm-kin0000000')`, r.id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func queued(t *testing.T, st *Store) map[string]Queued {
	t.Helper()
	q, err := st.Queue(context.Background(), "pending")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Queued{}
	for _, r := range q {
		out[r.ID] = r
	}
	return out
}

func ids(s []Sibling) string {
	var out []string
	for _, x := range s {
		out = append(out, x.ID)
	}
	return strings.Join(out, " ")
}

// SatVision: a member's flash dump and photos, then the same files sent
// again anonymously from the same network; a member's ipctool output, then
// the same output again from ipctool. Both anonymous reports are exact
// duplicates; the member's are not duplicates of them.
func TestTheSameReportSentTwiceIsADuplicateOfTheOneKept(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	mac, die, net := map[string]string{"mac": "a1a1a1a1a1a1a1a1", "die_id": "d1d1d1d1d1d1d1d1"}, "d1d1d1d1d1d1d1d1", "c0ffeec0ffeec0ff"
	e.seed(t,
		seed{id: "r-aydpbvki", ago: 5 * time.Hour, member: true, yaml: "chip: a\n", ids: mac, client: net, files: []string{"flash", "p1", "p2"}},
		seed{id: "r-u4qsp548", ago: 4 * time.Hour, client: net, files: []string{"p2", "flash", "p1"}},
		seed{id: "r-yp3eam2n", ago: 3 * time.Hour, member: true, yaml: "chip: b\n", ids: map[string]string{"mac": mac["mac"]}, client: net},
		seed{id: "r-pvhvgyqf", ago: 2 * time.Hour, yaml: "chip: b\n", ids: map[string]string{"die_id": die}, client: net},
		// the same network two days earlier is not the same sender
		seed{id: "r-oldnet22", ago: 60 * time.Hour, yaml: "chip: z\n", client: net},
	)
	if err := st.Link(ctx, "r-aydpbvki", "xiongmai-50h20l", "test"); err != nil {
		t.Fatal(err)
	}
	if err := st.Review(ctx, "r-aydpbvki", "publish", "test", ""); err != nil {
		t.Fatal(err)
	}
	q := queued(t, st)

	anon := q["r-u4qsp548"]
	if anon.DuplicateOf == nil || anon.DuplicateOf.ID != "r-aydpbvki" || anon.DuplicateOf.Status != "published" ||
		len(anon.DuplicateOf.Boards) != 1 || anon.DuplicateOf.Boards[0].ID != "xiongmai-50h20l" {
		t.Errorf("the anonymous dump is not a duplicate of the published one: %+v", anon.DuplicateOf)
	}
	if len(anon.SameCamera) != 0 || ids(anon.SameSender) != "r-aydpbvki r-yp3eam2n r-pvhvgyqf" {
		t.Errorf("no YAML, no identifiers: only the network says who sent it: camera %q sender %q", ids(anon.SameCamera), ids(anon.SameSender))
	}

	again := q["r-pvhvgyqf"]
	if again.DuplicateOf == nil || again.DuplicateOf.ID != "r-yp3eam2n" || again.DuplicateOf.Status != "pending" {
		t.Errorf("ipctool's output sent again is not a duplicate of the member's: %+v", again.DuplicateOf)
	}
	if ids(again.SameCamera) != "r-aydpbvki" {
		t.Errorf("the die ID's hash names one other report: %q", ids(again.SameCamera))
	}
	if again.SameCamera[0].Status != "published" || len(again.SameCamera[0].Boards) != 1 {
		t.Errorf("the same camera's published report does not say where: %+v", again.SameCamera[0])
	}
	if ids(again.SameSender) != "r-u4qsp548 r-yp3eam2n" {
		t.Errorf("the same network lists what the camera already does: %q", ids(again.SameSender))
	}

	if m := q["r-yp3eam2n"]; m.DuplicateOf != nil {
		t.Errorf("the member's report, holding the same, is the one kept: %+v", m.DuplicateOf)
	} else if ids(m.SameCamera) != "r-aydpbvki" {
		t.Errorf("the MAC's hash: %q", ids(m.SameCamera))
	}

	// The browser is told report ids, never what matched them.
	b, _ := json.Marshal(q)
	for _, secret := range []string{mac["mac"], die, net} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the queue shows a hash: %s", secret)
		}
	}
}

// TECKIN: an agent coming back to one camera with more each time. All the
// same camera, none a duplicate: each holds something the others lack.
func TestFollowUpsAreTheSameCameraAndNotDuplicates(t *testing.T) {
	e := newEnv(t)
	st := &Store{DB: e.pool}
	mac := map[string]string{"mac": "7e7e7e7e7e7e7e7e"}
	e.seed(t,
		seed{id: "r-teckin01", ago: 9 * time.Hour, yaml: "chip: t31\n", ids: mac, client: "aaaa", files: []string{"boot"}},
		seed{id: "r-teckin02", ago: 8 * time.Hour, yaml: "chip: t31\nsensor: x\n", ids: mac, client: "bbbb", files: []string{"boot", "uenv"}},
		seed{id: "r-teckin03", ago: 7 * time.Hour, ids: mac, client: "cccc", files: []string{"boot"}, note: "the UART pins, photographed"},
		seed{id: "r-teckin04", ago: 6 * time.Hour, ids: mac, client: "dddd", files: []string{"dump"}},
	)
	q := queued(t, st)
	for id, others := range map[string]string{
		"r-teckin01": "r-teckin02 r-teckin03 r-teckin04",
		"r-teckin02": "r-teckin01 r-teckin03 r-teckin04",
		"r-teckin03": "r-teckin01 r-teckin02 r-teckin04",
		"r-teckin04": "r-teckin01 r-teckin02 r-teckin03",
	} {
		r := q[id]
		if ids(r.SameCamera) != others {
			t.Errorf("%s: same camera %q, want %q", id, ids(r.SameCamera), others)
		}
		if r.DuplicateOf != nil {
			t.Errorf("%s: a follow-up is not a duplicate: %+v", id, r.DuplicateOf)
		}
		if len(r.SameSender) != 0 {
			t.Errorf("%s: other networks: %q", id, ids(r.SameSender))
		}
	}
}

// A report that holds less is a duplicate of a newer one that holds more,
// but never of one rejected.
func TestAReportHoldingLessIsADuplicateOfOneHoldingMoreUnlessRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	st := &Store{DB: e.pool}
	e.seed(t,
		seed{id: "r-less2222", ago: 3 * time.Hour, client: "aaaa", files: []string{"photo"}},
		seed{id: "r-more2222", ago: 2 * time.Hour, client: "bbbb", yaml: "chip: x\n", files: []string{"photo", "boot"}, note: "more"},
		seed{id: "r-copy2222", ago: 1 * time.Hour, client: "cccc", files: []string{"boot"}},
	)
	q := queued(t, st)
	if d := q["r-less2222"].DuplicateOf; d == nil || d.ID != "r-more2222" {
		t.Errorf("the photo alone is in the newer report: %+v", d)
	}
	if d := q["r-more2222"].DuplicateOf; d != nil {
		t.Errorf("the report holding more is nobody's duplicate: %+v", d)
	}
	if err := st.Review(ctx, "r-more2222", "reject", "test", ""); err != nil {
		t.Fatal(err)
	}
	q = queued(t, st)
	if d := q["r-copy2222"].DuplicateOf; d != nil {
		t.Errorf("a rejected report is nobody's original: %+v", d)
	}
}

// A note is the same note or none: a short one is not "in" a longer one
// that happens to contain its letters.
func TestAShortNoteInsideALongerOneIsNotADuplicate(t *testing.T) {
	e := newEnv(t)
	st := &Store{DB: e.pool}
	e.seed(t,
		seed{id: "r-longnote", ago: 2 * time.Hour, client: "aaaa", files: []string{"boot"}, note: "flashed fw v2.6, then took the board out"},
		seed{id: "r-shortnot", ago: 1 * time.Hour, client: "bbbb", files: []string{"boot"}, note: "v2"},
		seed{id: "r-samenote", ago: 30 * time.Minute, client: "cccc", files: []string{"boot"}, note: "  flashed fw v2.6,\nthen took the board   out "},
	)
	q := queued(t, st)
	if d := q["r-shortnot"].DuplicateOf; d != nil {
		t.Errorf("a note the other lacks is lost by rejecting: %+v", d)
	}
	if d := q["r-samenote"].DuplicateOf; d == nil || d.ID != "r-longnote" {
		t.Errorf("the same note, spaced otherwise, is the same note: %+v", d)
	}
}

// A report of a note alone, naming its board, sent twice.
func TestANoteSentTwiceIsADuplicate(t *testing.T) {
	e := newEnv(t)
	st := &Store{DB: e.pool}
	e.seed(t,
		seed{id: "r-noteonly", ago: 2 * time.Hour, client: "aaaa", note: "same board as the 50H20L, other lens"},
		seed{id: "r-noteagan", ago: 1 * time.Hour, client: "bbbb", note: "same board as the 50H20L, other lens"},
		seed{id: "r-noteelse", ago: 1 * time.Hour, client: "cccc", note: "a different camera"},
	)
	q := queued(t, st)
	if d := q["r-noteagan"].DuplicateOf; d == nil || d.ID != "r-noteonly" {
		t.Errorf("the note sent again: %+v", d)
	}
	if d := q["r-noteonly"].DuplicateOf; d != nil {
		t.Errorf("the first is kept: %+v", d)
	}
	if d := q["r-noteelse"].DuplicateOf; d != nil {
		t.Errorf("another note: %+v", d)
	}
}
