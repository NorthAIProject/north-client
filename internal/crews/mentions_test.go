package crews

import (
	"slices"
	"testing"
)

func TestParseMentions(t *testing.T) {
	for _, tc := range []struct {
		body    string
		handles []string
		all     bool
	}{
		{"@ana hi @Leo", []string{"ana", "leo"}, false},
		{"mail a@b.com", nil, false},
		{"@everyone go", nil, true},
		{"@a", nil, false},
		{"@ana @ana", []string{"ana"}, false},
		{"(@ana_b), nice", []string{"ana_b"}, false},
	} {
		handles, all := parseMentions(tc.body)
		if !slices.Equal(handles, tc.handles) || all != tc.all {
			t.Errorf("parseMentions(%q) = %v, %v; want %v, %v", tc.body, handles, all, tc.handles, tc.all)
		}
	}
}
