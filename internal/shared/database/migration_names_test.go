package database_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/NorthAIProject/north-client/migrations"
)

// The failure this guards against is the quiet one.
//
// Goose derives a migration's version by taking everything before the first
// underscore and calling strconv.ParseInt. A name the README once recommended —
// 20260808T1430_add_thing.sql — does not parse, and goose then leaves the file
// out of the run rather than refusing to start. Migrate returns no error, the
// column the migration creates is simply never created, and the first symptom
// is an "column does not exist" error pointing at a migration that visibly
// creates it.
//
// It cost an afternoon once. Reading the embedded filesystem takes a
// millisecond, so it should never cost one again.
func TestEveryMigrationFilenameIsParseable(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	seen := make(map[int64]string, len(entries))
	count := 0

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		count++

		version, err := goose.NumericComponent(name)
		if err != nil {
			t.Errorf("%s would be skipped by goose, not applied: %v\n"+
				"    Use digits only, no 'T' separator: date -u +%%Y%%m%%d%%H%%M%%S", name, err)
			continue
		}

		if other, clash := seen[version]; clash {
			t.Errorf("%s and %s share version %d; one of them will never run", name, other, version)
		}
		seen[version] = name
	}

	if count == 0 {
		t.Fatal("no .sql migrations were embedded; check the //go:embed pattern in migrations/fs.go")
	}
}
