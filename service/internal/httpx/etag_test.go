package httpx

import (
	"net/http"
	"testing"
)

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
		{`, "13c177f64ce8057e992861fb",`, true}, // empty list elements are allowed
		{`"13c177f64ce8057e992861fc"`, false},
		{`W/"13c177f64ce8057e"`, false},
		{``, false},
		{`,`, false},
		{`13c177f64ce8057e992861fb`, false},  // not an entity-tag
		{`"13c177f64ce8057e992861fb`, false}, // unterminated
		{`"a,b"`, false},                     // a comma inside a tag is part of it
		// Malformed values match nothing, even when they hold the tag.
		{`"13c177f64ce8057e992861fb"junk`, false},
		{`"13c177f64ce8057e992861fb" "other"`, false},
		{`"13c177f64ce8057e992861fb", junk`, false},
		// "*" is decided before a handler has looked the resource up, so it
		// would answer 304 for one that does not exist: it matches nothing.
		{`*`, false},
		{`*junk`, false},
		{`*, "13c177f64ce8057e992861fb"`, false},
	} {
		if got := ETagMatches(c.header, strong); got != c.want {
			t.Errorf("ETagMatches(%q) = %v, want %v", c.header, got, c.want)
		}
	}
	// A weak ETag of our own matches its strong spelling too.
	if !ETagMatches(`"abc"`, `W/"abc"`) || !ETagMatches(`W/"abc"`, `W/"abc"`) {
		t.Error("a weak ETag of ours did not match")
	}
	if ETagMatches(`"abc"`, ``) || ETagMatches(`"abc"`, `abc`) {
		t.Error("an ETag that is not one matched something")
	}
}

// Every If-None-Match field counts, not only the first.
func TestEveryIfNoneMatchFieldIsRead(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.Header.Add("If-None-Match", `"old"`)
	r.Header.Add("If-None-Match", `W/"current"`)
	if !Revisited(r, `"current"`) {
		t.Error("a tag in the second field was not recognised")
	}
	if Revisited(r, `"other"`) {
		t.Error("a tag in no field matched")
	}
	empty, _ := http.NewRequest(http.MethodGet, "/", nil)
	empty.Header.Add("If-None-Match", ``)
	empty.Header.Add("If-None-Match", `"current"`)
	if !Revisited(empty, `"current"`) {
		t.Error("an empty first field hid the second")
	}
	if Revisited(&http.Request{Header: http.Header{}}, `"current"`) {
		t.Error("no header matched")
	}
}
