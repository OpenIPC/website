package httpx

import "testing"

// A revisit must be recognised however the tag came back. The case that
// failed in production: nginx's gzip weakened the service's strong ETag, the
// client returned it weak, and an exact comparison answered 200 every time.
func TestARevisitIsRecognisedWeakOrStrong(t *testing.T) {
	strong := `"13c177f64ce8057e992861fb"`
	for _, c := range []struct {
		header string
		want   bool
	}{
		{strong, true},
		{`W/"13c177f64ce8057e992861fb"`, true}, // what came back through nginx
		{`"other", W/"13c177f64ce8057e992861fb"`, true},
		{` "other" ,  "13c177f64ce8057e992861fb" `, true},
		{`*`, true},
		{`"13c177f64ce8057e992861fc"`, false},
		{`W/"13c177f64ce8057e"`, false},
		{``, false},
		{`13c177f64ce8057e992861fb`, false},  // not an entity-tag
		{`"13c177f64ce8057e992861fb`, false}, // unterminated
		{`"a,b"`, false},                     // a comma inside a tag is part of it
	} {
		if got := ETagMatches(c.header, strong); got != c.want {
			t.Errorf("ETagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
	// A weak ETag of our own matches its strong spelling too.
	if !ETagMatches(`"abc"`, `W/"abc"`) || !ETagMatches(`W/"abc"`, `W/"abc"`) {
		t.Error("a weak ETag of ours did not match")
	}
	if ETagMatches(`"abc"`, ``) {
		t.Error("no ETag matched something")
	}
}
