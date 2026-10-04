package club

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/OpenIPC/website/service/internal/wallstars"
)

// The member's cameras on the Open Wall and the leaderboard. Linking,
// settling and the ledger are internal/wallstars'; this is the member's side
// of it and the bot's messages about it.

func (a *API) wallHandlers() map[string]http.Handler {
	return map[string]http.Handler{
		"GET /api/v1/club/cameras":                  http.HandlerFunc(a.cameras),
		"POST /api/v1/club/cameras/code":            a.post(a.cameraCode),
		"POST /api/v1/club/cameras/{camera}/unlink": a.post(a.unlinkCamera),
		"POST /api/v1/club/cameras/{camera}/owner":  a.post(a.cameraOwner),
		"POST /api/v1/club/listed":                  a.post(a.listed),
		"GET /api/v1/club/leaderboard":              http.HandlerFunc(a.leaderboard),
	}
}

// cameras is GET /api/v1/club/cameras: the member's linked cameras, the code
// they can link another with, and whether they are on the leaderboard.
func (a *API) cameras(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	cams, err := a.Wall.Cameras(r.Context(), m.ID, a.now())
	if err != nil {
		a.fail(w, "the cameras", err)
		return
	}
	code, err := a.Wall.CurrentCode(r.Context(), m.ID)
	if err != nil {
		a.fail(w, "the code", err)
		return
	}
	listed, err := a.Wall.Listed(r.Context(), m.ID)
	if err != nil {
		a.fail(w, "the member", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cameras": cams, "code": code, "listed": listed, "max_cameras": wallstars.MaxCameras})
}

// cameraCode is POST /api/v1/club/cameras/code: a fresh code to link a camera.
func (a *API) cameraCode(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	code, err := a.Wall.NewCode(r.Context(), m.ID)
	if errors.Is(err, wallstars.ErrTooMany) {
		a.refuse(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		a.fail(w, "the code", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code})
}

func (a *API) unlinkCamera(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	done, err := a.Wall.Unlink(r.Context(), m.ID, r.PathValue("camera"))
	if err != nil {
		a.fail(w, "unlinking", err)
		return
	}
	if !done {
		a.refuse(w, http.StatusNotFound, "that camera is not linked to you")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unlinked": true})
}

// cameraOwner is POST /api/v1/club/cameras/{camera}/owner {"show": true}:
// the member's name on the camera's wall page, or not.
func (a *API) cameraOwner(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	var in struct {
		Show bool `json:"show"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"show": true} or {"show": false}`)
		return
	}
	done, err := a.Wall.ShowOwner(r.Context(), m.ID, r.PathValue("camera"), in.Show)
	if err != nil {
		a.fail(w, "the setting", err)
		return
	}
	if !done {
		a.refuse(w, http.StatusNotFound, "that camera is not linked to you")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"show": in.Show})
}

// listed is POST /api/v1/club/listed {"listed": true}: on the leaderboard or off.
func (a *API) listed(w http.ResponseWriter, r *http.Request) {
	m, ok := a.signedIn(w, r)
	if !ok {
		return
	}
	var in struct {
		Listed bool `json:"listed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
		a.refuse(w, http.StatusBadRequest, `send {"listed": true} or {"listed": false}`)
		return
	}
	if err := a.Wall.SetListed(r.Context(), m.ID, in.Listed); err != nil {
		a.fail(w, "the setting", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"listed": in.Listed})
}

// leaderboard is GET /api/v1/club/leaderboard?period=all|30d: public, and
// only members who asked to be on it.
func (a *API) leaderboard(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	period := r.URL.Query().Get("period")
	switch period {
	case "", "all":
		period = "all"
	case "30d":
		since = a.now().Add(-30 * 24 * time.Hour)
	default:
		a.refuse(w, http.StatusBadRequest, "period is all or 30d")
		return
	}
	you, err := a.session(r)
	if err != nil {
		a.fail(w, "the session", err)
		return
	}
	rows, err := a.Wall.Leaderboard(r.Context(), since, you)
	if err != nil {
		a.fail(w, "the leaderboard", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": period, "members": rows})
}

// NotifyLinked tells a member a camera of theirs was just linked.
func (a *API) NotifyLinked(ctx context.Context, l wallstars.Linked) {
	a.notify(ctx, l.Member, func(loc string) string {
		return fmt.Sprintf(t(loc, "camera_linked"), cameraName(loc, l.Name), wallstars.JoinDays)
	}, a.camerasButton)
}

// SettleWall runs the settlement and tells each owner what it did.
func (a *API) SettleWall(ctx context.Context) (*wallstars.Settled, error) {
	res, err := a.Wall.Settle(ctx, a.now())
	if err != nil {
		return nil, err
	}
	for _, n := range res.Notices {
		a.notifyWall(ctx, n)
	}
	return res, nil
}

func (a *API) notifyWall(ctx context.Context, n wallstars.Notice) {
	total := 0
	if n.Kind == "stars" {
		var err error
		if total, err = a.totalStars(ctx, n.Member); err != nil {
			a.Log.Warn("club: stars unreadable for a notice", "member", n.Member, "err", err)
			return
		}
	}
	a.notify(ctx, n.Member, func(loc string) string {
		name := cameraName(loc, n.Camera)
		switch {
		case n.Kind == "stars" && n.Join && n.Rare:
			return fmt.Sprintf(t(loc, "camera_joined_rare"), n.Points, name, total)
		case n.Kind == "stars" && n.Join:
			return fmt.Sprintf(t(loc, "camera_joined"), n.Points, name, total)
		case n.Kind == "stars":
			return fmt.Sprintf(t(loc, "camera_month"), n.Points, name, total)
		case n.Kind == "milestone":
			return fmt.Sprintf(t(loc, "camera_milestone"), name, n.Days)
		default:
			return fmt.Sprintf(t(loc, "camera_silent"), name, n.Days)
		}
	}, a.camerasButton)
}

func (a *API) camerasButton(loc string) []Button {
	return []Button{{Text: t(loc, "open_cameras"), URL: a.Cfg.SiteURL + "/club/#cameras"}}
}

// cameraName is a camera as a message names it: its caption, escaped for
// Telegram's HTML, or a plain "your camera".
func cameraName(loc, name string) string {
	if name == "" {
		return t(loc, "a_camera")
	}
	return "“" + html.EscapeString(name) + "”"
}

// totalStars is a member's stars from both ledgers.
func (a *API) totalStars(ctx context.Context, member string) (int, error) {
	reports, err := a.Reports.Store().StarsOf(ctx, member)
	if err != nil {
		return 0, err
	}
	if a.Wall == nil {
		return reports, nil
	}
	wall, err := a.Wall.StarsOf(ctx, member)
	return reports + wall, err
}
