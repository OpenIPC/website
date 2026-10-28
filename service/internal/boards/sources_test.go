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

// A model only Haixin's catalogue knows still has a card with its name and
// description; where an earlier source speaks too, that source's words win.
func TestAHaixinOnlyModelHasACardSummary(t *testing.T) {
	str := func(s string) *string { return &s }
	m := &modelJSON{ID: "haixin-hx3001s-1080k34-38-b", About: []*aboutJSON{{
		Source: "haixin", Locale: "en", Name: str("2MP HD module"), Description: str("The 2MP low lux HD 38 camera module.\nMore."),
	}}}
	summarise(m)
	if m.Summary == nil || m.Summary.Name == nil || *m.Summary.Name != "2MP HD module" ||
		m.Summary.Lead == nil || *m.Summary.Lead != "The 2MP low lux HD 38 camera module." {
		t.Fatalf("summary %+v", m.Summary)
	}
	m = &modelJSON{ID: "x", About: []*aboutJSON{
		{Source: "haixin", Locale: "en", Name: str("Haixin's name")},
		{Source: "xiongmai", Locale: "en", Name: str("Xiongmai's name")},
	}}
	summarise(m)
	if m.Summary == nil || *m.Summary.Name != "Xiongmai's name" {
		t.Fatalf("an earlier source does not win: %+v", m.Summary)
	}
}
