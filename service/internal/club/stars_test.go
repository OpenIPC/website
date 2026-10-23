package club

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// fileOf is the file of a report as its sender's page lists it, by name.
func fileOf(t *testing.T, r map[string]any, name string) map[string]any {
	t.Helper()
	for _, f := range r["files"].([]any) {
		if f := f.(map[string]any); f["name"] == name {
			return f
		}
	}
	t.Fatalf("%s has no %s: %v", r["id"], name, r["files"])
	return nil
}

// Stars per camera, not per report (2026-10-23: a member was paid 11 for a
// programmer's dump, a boot log, photos and ipctool's output of a camera,
// and 11 again for ipctool's output and backup of the same camera). One
// member's reports about one camera -- the same MAC in ipctool's output --
// pay ipctool's output once and the dumps up to the best of them once; the
// same photo twice pays once; another camera of theirs pays as ever. And
// rejecting and publishing again in any order leaves the camera paid
// exactly once.
func TestAMemberIsPaidOncePerCamera(t *testing.T) {
	e := newEnv(t)
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	maint := e.maintainer(t)
	yaml, err := os.ReadFile("../reports/testdata/hi3516cv300-imx291.txt")
	if err != nil {
		t.Fatal(err)
	}
	backupOf := func(yaml []byte, b byte) []byte {
		return append(append(append([]byte{}, yaml...), 0), 4, 0, 0, 0, b, b, b, b)
	}
	decide := func(id, decision string, points, total float64) {
		t.Helper()
		code, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": decision, "models": []string{"anjoy-ms-j10"}})
		if code != 200 || d["points"].(float64) != points || d["total"].(float64) != total {
			t.Fatalf("%s %s: %d %v, want %v, %v in total", decision, id, code, d, points, total)
		}
	}
	stars := func(id string) float64 { return e.status(t, ivan, id)["stars"].(float64) }

	// A: ipctool's output, a programmer's dump, a boot log and two photos:
	// 1 + 3 + 1 + 2.
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10", "yaml": string(yaml)},
		map[string][]byte{"backup": bytes.Repeat([]byte{0x11}, 1<<20), "boot_log": []byte("U-Boot 2016.11\n"), "photo": photoN(1), "photo#2": photoN(2)})
	a := out["id"].(string)
	decide(a, "publish", 7, 7)

	// B: ipctool's output and its backup of the same camera. The output is
	// paid already, and the backup earns what the dump did not: 10 - 3.
	_, out = ivan.send(t, map[string]string{"channel": "web", "yaml": string(yaml)}, map[string][]byte{"backup": backupOf(yaml, 'b')})
	b := out["id"].(string)
	r := e.status(t, ivan, b)
	if r["pending"].(float64) != 7 || r["yaml_points"].(float64) != 0 || r["yaml_reason"] != "camera" {
		t.Fatalf("B waiting: %v", r)
	}
	if f := fileOf(t, r, "backup.bin"); f["points"].(float64) != 7 || f["reason"] != "camera" {
		t.Errorf("B's backup: %v", f)
	}
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	if queued := q["reports"].([]any)[0].(map[string]any); queued["id"] != b || queued["potential"].(float64) != 7 || queued["yaml_reason"] != "camera" {
		t.Errorf("the queue: %v", queued)
	}
	decide(b, "publish", 7, 14)
	// The bot says why it earned less, not that the catalogue had it.
	told := func(who string, want ...string) {
		t.Helper()
		text, _ := e.tg.last(t)
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s was told %q, without %q", who, text, w)
			}
		}
	}
	told("Ivan", "+7 ★", "you were already paid for this camera")

	// C: the same two photos again, and one more: only the new one earns.
	_, out = ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"photo": photoN(1), "photo#2": photoN(2), "photo#3": photoN(3)})
	c := out["id"].(string)
	r = e.status(t, ivan, c)
	if r["pending"].(float64) != 1 || fileOf(t, r, "photo.bin")["reason"] != "known" || fileOf(t, r, "photo_3.bin")["points"].(float64) != 1 {
		t.Fatalf("C waiting: %v", r)
	}
	decide(c, "publish", 1, 15)
	told("Ivan", "+1 ★", "the same file was already published")
	// Nor does another member sending the same photo earn with it, and the
	// bot tells him why in his language.
	petr := e.browser("198.51.100.2")
	_, login := petr.json(t, "POST", "/api/v1/club/telegram", nil)
	e.startAndConfirm(t, 888, "petr", "ru", codeOf(t, login["link"].(string)))
	if _, out := petr.json(t, "GET", "/api/v1/club/login", nil); out["state"] != "signed_in" {
		t.Fatalf("Petr's sign-in: %v", out)
	}
	_, out = petr.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"photo": photoN(1)})
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+out["id"].(string), map[string]any{"decision": "publish"}); code != 200 || d["points"].(float64) != 0 {
		t.Errorf("Petr's copy of Ivan's photo: %d %v", code, d)
	}
	told("Petr", "звёзд в этот раз нет", "такой же файл уже опубликован")

	// Another camera of Ivan's, its own MAC: paid in full, 1 + 10.
	other := []byte(strings.Replace(string(yaml), "00:12:31:5e:e0:d2", "00:12:31:5e:e0:d3", 1))
	_, out = ivan.send(t, map[string]string{"channel": "web", "yaml": string(other)}, map[string][]byte{"backup": backupOf(other, 'e')})
	decide(out["id"].(string), "publish", 11, 26)

	// A rejected: what it held of the camera goes to B, which is then paid
	// as if A had never been -- 1 + 10 -- and A's own files go.
	decide(a, "reject", -7+4, 23)
	if stars(b) != 11 || stars(a) != 0 {
		t.Errorf("after rejecting A: A %v, B %v", stars(a), stars(b))
	}
	// Rejected, A shows what it holds -- nothing -- not what it would earn.
	if r := e.status(t, ivan, a); r["yaml_points"].(float64) != 0 || fileOf(t, r, "boot_log.bin")["points"].(float64) != 0 || r["pending"].(float64) != 0 {
		t.Errorf("A rejected: %v", r)
	}
	// A published again: B holds the output and the best dump, so A earns
	// its boot log and photos only. The camera is paid once, whole.
	decide(a, "publish", 3, 26)
	r = e.status(t, ivan, a)
	if fileOf(t, r, "flash.bin")["points"].(float64) != 0 || fileOf(t, r, "flash.bin")["reason"] != "camera" || r["yaml_reason"] != "camera" {
		t.Errorf("A published again: %v", r)
	}
	decide(a, "publish", 0, 26)
	told("Ivan", "you were already paid for this camera")

	// B rejected: A is the camera's again, its output and its dump.
	decide(b, "reject", -11+4, 19)
	if stars(a) != 7 {
		t.Errorf("A after rejecting B: %v", stars(a))
	}
	// And B published again earns the backup's difference once more.
	decide(b, "publish", 7, 26)
	if stars(a)+stars(b) != 14 {
		t.Errorf("the camera holds %v + %v", stars(a), stars(b))
	}
}

// A takedown keeps the stars a report was paid -- its owner's privacy, not
// a penalty -- and the camera stays paid for, though the report's
// identifiers are blanked: a later report of the camera earns only what the
// withdrawn one did not.
func TestATakenDownReportStillHoldsItsCamera(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	maint := e.maintainer(t)
	yaml, err := os.ReadFile("../reports/testdata/hi3516cv300-imx291.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10", "yaml": string(yaml)},
		map[string][]byte{"backup": bytes.Repeat([]byte{0x22}, 1<<20)})
	a := out["id"].(string)
	if _, d := maint.json(t, "POST", "/api/v1/club/review/"+a, map[string]any{"decision": "publish"}); d["points"].(float64) != 4 {
		t.Fatalf("A: %v", d)
	}
	if _, err := e.api.Reports.Store().Takedown(ctx, a, "maint", "owner asked"); err != nil {
		t.Fatal(err)
	}
	if m := ivan.me(t); m["stars"].(float64) != 4 {
		t.Errorf("the takedown took the stars: %v", m["stars"])
	}
	backup := append(append(append([]byte{}, yaml...), 0), 4, 0, 0, 0, 'b', 'o', 'o', 't')
	_, out = ivan.send(t, map[string]string{"channel": "web", "yaml": string(yaml)}, map[string][]byte{"backup": backup})
	b := out["id"].(string)
	if r := e.status(t, ivan, b); r["pending"].(float64) != 7 || r["yaml_reason"] != "camera" {
		t.Errorf("B waiting: %v", r)
	}
	if _, d := maint.json(t, "POST", "/api/v1/club/review/"+b, map[string]any{"decision": "publish", "models": []string{"anjoy-ms-j10"}}); d["points"].(float64) != 7 || d["total"].(float64) != 11 {
		t.Errorf("B after A's takedown: %v", d)
	}
}
