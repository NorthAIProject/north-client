package search_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/search"
)

// The .sql files sqlc reads cannot import Go, so the constants in rank.go
// cannot be interpolated into them. This test is the enforcement instead: it
// reads every query and migration in the repository and checks that the
// full-text expressions are spelled the one way the indexes were built for.
//
// It is worth a test rather than a code review note because the failure is
// silent. A query that says 'simple' where the index says 'english' still
// returns rows — it just stops using the index and scans every row the user
// owns, and nothing anywhere reports that it happened.

var (
	// Any to_tsvector call whose first argument is not the canonical config.
	wrongVectorConfig = regexp.MustCompile(`to_tsvector\(\s*(?:'([a-z_]+)'\s*,)?`)

	// tsquery constructors that must never appear: to_tsquery raises a syntax
	// error on ordinary punctuation, and plainto_tsquery silently discards the
	// phrase and negation syntax people actually type.
	unsafeTSQuery = regexp.MustCompile(`(^|[^a-z_])(to_tsquery|plainto_tsquery|phraseto_tsquery)\s*\(`)
)

func TestSQLUsesTheCanonicalTextSearchConfig(t *testing.T) {
	for _, file := range sqlFiles(t) {
		body := readFile(t, file)
		if !strings.Contains(body, "to_tsvector") && !strings.Contains(body, "tsquery") {
			continue
		}

		for _, m := range wrongVectorConfig.FindAllStringSubmatch(body, -1) {
			switch m[1] {
			case search.Config:
			case "":
				t.Errorf("%s: to_tsvector called without a text search configuration; "+
					"it would then depend on default_text_search_config and not match the index", rel(file))
			default:
				t.Errorf("%s: to_tsvector uses %q, but the indexes are built on %q",
					rel(file), m[1], search.Config)
			}
		}

		if loc := unsafeTSQuery.FindStringIndex(body); loc != nil {
			t.Errorf("%s: uses %s; only websearch_to_tsquery is safe for user input (see internal/search/query.go)",
				rel(file), strings.TrimSuffix(strings.TrimSpace(body[loc[0]:loc[1]]), "("))
		}
	}
}

// TestRankExprMatchesTheIndexedExpression keeps rank.go honest about the
// migration it claims to mirror. If the two drift, every query built from the
// constants stops using the index.
func TestRankExprMatchesTheIndexedExpression(t *testing.T) {
	if !strings.Contains(search.RankExpr, search.VectorExpr) {
		t.Errorf("RankExpr does not contain VectorExpr:\n  rank:   %s\n  vector: %s",
			search.RankExpr, search.VectorExpr)
	}

	migration := readFile(t, repoPath(t, "migrations", "20260808090000_add_memory_and_message_search.sql"))
	if !strings.Contains(migration, search.VectorExpr) {
		t.Errorf("the search migration does not contain VectorExpr %q; the expression indexes and "+
			"internal/search/rank.go have drifted apart", search.VectorExpr)
	}
}

func sqlFiles(t *testing.T) []string {
	t.Helper()

	var out []string
	for _, root := range []string{repoPath(t, "internal"), repoPath(t, "migrations")} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".sql") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(out) == 0 {
		t.Fatal("found no .sql files; the repository layout this test assumes has changed")
	}
	return out
}

func repoPath(t *testing.T, parts ...string) string {
	t.Helper()
	return filepath.Join(append([]string{"..", ".."}, parts...)...)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func rel(path string) string { return filepath.ToSlash(strings.TrimPrefix(path, "../../")) }
