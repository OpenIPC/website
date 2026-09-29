package boards

import (
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// For the reports' survival test (survival_test.go, package boards_test),
// which imports internal/reports and so cannot live in package boards: the
// fixtures every importer test here uses.

func Imported(t *testing.T) (*pgxpool.Pool, string) { return imported(t) }

func Donor(t *testing.T, id string, models ...map[string]any) fstest.MapFS {
	return donor(t, id, models...)
}

func DonorModel(maker, code string, extra map[string]any) map[string]any {
	return model(maker, code, extra)
}

func Supported(label string) string { return supported(label) }

func Archive() fstest.MapFS { return archive() }

func JPG(w, h int) []byte { return jpg(w, h) }
