package exercises

import (
	"net/http/httptest"
	"testing"
)

// Page numbers arrive from a URL someone may have typed, edited or truncated,
// so every malformed form has to land somewhere sensible rather than 400 or
// produce a negative offset that Postgres rejects.
func TestPageParamFallsBackToTheFirstPage(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"":            1,
		"?page=":      1,
		"?page=0":     1,
		"?page=-3":    1,
		"?page=abc":   1,
		"?page=1":     1,
		"?page=7":     7,
		"?page=00012": 12,
	}

	for query, want := range cases {
		r := httptest.NewRequest("GET", "/app/exercises"+query, nil)
		if got := pageParam(r); got != want {
			t.Errorf("pageParam(%q) = %d, want %d", query, got, want)
		}
	}
}

// lastPage is never 0: an empty result still renders a page 1 for the "nothing
// matched" message, and a 0 would make the handler's clamp ask for a negative
// offset.
func TestLastPageIsAtLeastOneAndRoundsUp(t *testing.T) {
	t.Parallel()

	cases := map[int]int{
		0:            1,
		-1:           1,
		1:            1,
		PageSize:     1,
		PageSize + 1: 2,
		455:          (455 + PageSize - 1) / PageSize,
	}

	for total, want := range cases {
		if got := lastPage(total); got != want {
			t.Errorf("lastPage(%d) = %d, want %d", total, got, want)
		}
	}
}
