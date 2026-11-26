package club

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var claimCode = regexp.MustCompile(`^claim-[23456789A-HJ-NP-Z]{4}-[23456789A-HJ-NP-Z]{4}-[23456789A-HJ-NP-Z]{4}$`)

// Reports sent without a member -- ipctool's, and the send form's signed
// out with a board -- are answered with a claim code that only its sha256
// is kept of. A member who enters it on /club makes the report theirs, as
// if sent signed in, once; a code mistyped, unknown or used is refused.
func TestAReportSentSignedOutIsClaimedFromTheClub(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	status, up := e.ipctoolUpload(t, map[string]string{"note": "bought on a market"})
	if status != http.StatusCreated {
		t.Fatalf("ipctool upload: %d %v", status, up)
	}
	ipctool := up["id"].(string)
	code, _ := up["claim_code"].(string)
	if !claimCode.MatchString(code) || !strings.Contains(up["next"].(string), code) || !strings.Contains(up["next"].(string), "/club/") {
		t.Fatalf("ipctool is not told how to claim its report: %v", up)
	}
	sum := sha256.Sum256([]byte(code))
	var kept, plain int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM report_claims WHERE report_id = $1 AND code_sha256 = $2`, ipctool, hex.EncodeToString(sum[:])).Scan(&kept)
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM report_claims WHERE code_sha256 = $1`, code).Scan(&plain)
	if kept != 1 || plain != 0 {
		t.Errorf("the code's sha256 is kept %d times, the code %d", kept, plain)
	}

	guest := e.browser("198.51.100.9")
	status, sent := guest.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"boot_log": []byte("U-Boot 2016.11\n")})
	if status != http.StatusCreated || !claimCode.MatchString(sent["claim_code"].(string)) {
		t.Fatalf("a guest's send: %d %v", status, sent)
	}
	web := sent["id"].(string)

	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	if _, mine := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10"}, map[string][]byte{"boot_log": []byte("U-Boot 2016.11\n")}); mine["claim_code"] != nil {
		t.Errorf("a member's send is given a claim code: %v", mine)
	}
	if status, _ := guest.json(t, "POST", "/api/v1/club/reports/claim", map[string]string{"code": code}); status != http.StatusUnauthorized {
		t.Errorf("a claim signed out: %d", status)
	}

	// Typed in lower case, with spaces around it.
	status, c := ivan.json(t, "POST", "/api/v1/club/reports/claim", map[string]string{"code": " " + strings.ToLower(code) + " "})
	if status != 200 || c["id"] != ipctool || c["points"].(float64) != 0 {
		t.Fatalf("ivan's claim: %d %v", status, c)
	}
	if owner, _ := e.api.Reports.Store().Owner(ctx, ipctool); owner == "" {
		t.Error("the claimed report has no owner")
	}
	_, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil)
	listed := false
	for _, x := range mine["reports"].([]any) {
		if x := x.(map[string]any); x["id"] == ipctool {
			listed = x["pending"].(float64) > 0
		}
	}
	if !listed {
		t.Errorf("the claimed report is not ivan's, paying when accepted: %v", mine)
	}

	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	for _, tc := range []struct {
		code   string
		status int
	}{
		{code, http.StatusConflict},
		{"claim-2222-2222-2222", http.StatusNotFound},
		{"club-AAAA-AAAA", http.StatusBadRequest},
	} {
		if status, out := petr.json(t, "POST", "/api/v1/club/reports/claim", map[string]string{"code": tc.code}); status != tc.status {
			t.Errorf("claim %q: %d %v, want %d", tc.code, status, out, tc.status)
		}
	}

	// The guest's report named a board: its row is there, nobody's, and
	// the claim gives it its member and keeps the board.
	if status, c := petr.json(t, "POST", "/api/v1/club/reports/claim", map[string]string{"code": sent["claim_code"].(string)}); status != 200 || c["id"] != web {
		t.Fatalf("petr's claim: %d %v", status, c)
	}
	var member, model string
	_ = e.pool.QueryRow(ctx, `SELECT member_id, model_id FROM report_submissions WHERE report_id = $1`, web).Scan(&member, &model)
	if member != petr.me(t)["id"] || model != "anjoy-ms-j10" {
		t.Errorf("the guest's report: member %q, board %q", member, model)
	}
	// The guard still refuses any other change to it.
	if _, err := e.pool.Exec(ctx, `UPDATE report_submissions SET member_id = NULL, model_id = NULL WHERE report_id = $1`, web); err == nil {
		t.Error("the guard let a submission's board change")
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL openipc.report_claim = 'on'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE report_submissions SET member_id = $2 WHERE report_id = $1`, web, ivan.me(t)["id"]); err == nil {
		t.Error("the guard let a claimed report move to another member")
	}
	_ = tx.Rollback(ctx)

	// Accepted now, ivan's claimed report pays ivan.
	maint := e.maintainer(t)
	if status, d := maint.json(t, "POST", "/api/v1/club/review/"+ipctool, map[string]any{"decision": "publish", "models": []string{"anjoy-ms-j10"}}); status != 200 || d["points"].(float64) <= 0 {
		t.Fatalf("publish the claimed report: %d %v", status, d)
	}
}

// A report claimed after a maintainer published it is paid at once what
// its review would have paid a member, and only once.
func TestAClaimOfAPublishedReportPaysItsStars(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, up := e.ipctoolUpload(t, nil)
	id, code := up["id"].(string), up["claim_code"].(string)

	maint := e.maintainer(t)
	if status, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "models": []string{"anjoy-ms-j10"}}); status != 200 || d["points"].(float64) != 0 {
		t.Fatalf("publish nobody's report: %d %v", status, d)
	}

	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	status, c := ivan.json(t, "POST", "/api/v1/club/reports/claim", map[string]string{"code": code})
	if status != 200 || c["points"].(float64) <= 0 || c["total"] != c["points"] {
		t.Fatalf("claim a published report: %d %v", status, c)
	}
	if m := ivan.me(t); m["stars"] != c["total"] {
		t.Errorf("ivan's stars: %v, the claim said %v", m["stars"], c["total"])
	}
	var reason string
	_ = e.pool.QueryRow(ctx, `SELECT reason FROM report_stars WHERE report_id = $1 LIMIT 1`, id).Scan(&reason)
	if reason != "claimed by its sender" {
		t.Errorf("the ledger's reason: %q", reason)
	}

	// Published again, it pays nothing more.
	if status, d := maint.json(t, "POST", "/api/v1/club/review/"+id, map[string]any{"decision": "publish", "models": []string{"anjoy-ms-j10"}}); status != 200 || d["points"].(float64) != 0 {
		t.Errorf("publish it again: %d %v", status, d)
	}
}
