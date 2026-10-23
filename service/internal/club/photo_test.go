package club

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func pngPhoto(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.Gray{uint8((x ^ y) * 4)})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The review card's photo tools: the queue says whether the sender
// confirmed the photos are their own camera's and what each will be
// published as; a maintainer blurs one or keeps it private, a member cannot,
// and the sender sees the copy that will be published.
func TestAMaintainerBlursAPhotoFromTheReviewQueue(t *testing.T) {
	e := newEnv(t)
	ivan := e.signedIn(t, 777, "ivan_k", "198.51.100.1")
	code, out := ivan.send(t, map[string]string{"channel": "web", "model": "anjoy-ms-j10", "own_photos": "yes"},
		map[string][]byte{"photo": pngPhoto(t)})
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, out)
	}
	id := out["id"].(string)

	maint := e.browser("198.51.100.3")
	loc, _ := url.Parse(maint.do(t, "GET", "/api/v1/club/github", nil, "").Header().Get("Location"))
	maint.do(t, "GET", "/api/v1/club/github/callback?code=maint&state="+loc.Query().Get("state"), nil, "")
	_, q := maint.json(t, "GET", "/api/v1/club/review", nil)
	f := q["reports"].([]any)[0].(map[string]any)["file_list"].([]any)[0].(map[string]any)
	if f["own"] != true || f["public"] != "as_sent" || f["public_url"] != nil {
		t.Fatalf("the queue's photo: %v", f)
	}
	pos := strconv.Itoa(int(f["position"].(float64)))
	blur := map[string]any{"action": "blur", "regions": []map[string]float64{{"x": 0.25, "y": 0.25, "w": 0.5, "h": 0.5}}}

	if code, _ := ivan.json(t, "POST", "/api/v1/club/review/"+id+"/files/"+pos, blur); code != 403 {
		t.Errorf("a member blurred a photo: %d", code)
	}
	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id+"/files/"+pos, blur); code != 200 || d["public"] != "blurred" {
		t.Fatalf("blur: %d %v", code, d)
	}
	_, mine := ivan.json(t, "GET", "/api/v1/club/reports", nil)
	f = mine["reports"].([]any)[0].(map[string]any)["files"].([]any)[0].(map[string]any)
	if f["public"] != "blurred" || f["public_url"] == nil || len(f["regions"].([]any)) != 1 {
		t.Fatalf("the sender's photo: %v", f)
	}
	if rec := ivan.do(t, "GET", f["public_url"].(string), nil, ""); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("the sender's blurred copy: %d %v", rec.Code, rec.Header())
	}
	petr := e.signedIn(t, 888, "petr", "198.51.100.2")
	if rec := petr.do(t, "GET", f["public_url"].(string), nil, ""); rec.Code != 404 {
		t.Errorf("another member's blurred copy: %d", rec.Code)
	}

	if code, d := maint.json(t, "POST", "/api/v1/club/review/"+id+"/files/"+pos, map[string]any{"action": "private"}); code != 200 || d["public"] != "private" {
		t.Fatalf("private: %d %v", code, d)
	}
	_, q = maint.json(t, "GET", "/api/v1/club/review", nil)
	f = q["reports"].([]any)[0].(map[string]any)["file_list"].([]any)[0].(map[string]any)
	if f["public"] != "private" || f["private"] != true {
		t.Errorf("kept private: %v", f)
	}
}
