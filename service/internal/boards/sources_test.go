package boards

import (
	"encoding/json"
	"strings"
	"testing"
)

// A board no source says anything about and with no units (a PCB known only
// as another board's link) is still sent with a list of sources: null took
// the site's gallery down at the first card that counted them.
func TestABoardWithNothingStillListsItsSources(t *testing.T) {
	m := &modelJSON{ID: "maker-pcb"}
	summarise(m)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"sources":[]`) {
		t.Fatalf("sources is not an empty list: %s", b)
	}
}
