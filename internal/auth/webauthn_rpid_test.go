package auth

import "testing"

func TestRelyingPartyIDStripsEnvCommentsAndURLs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, base, want string
	}{
		{"", "http://localhost:8090", "localhost"},
		{"# optional; defaults to host of BASE_URL", "http://localhost:8090", "localhost"},
		{"          # optional; defaults to host of BASE_URL", "http://localhost:8090", "localhost"},
		{"http://localhost:8090", "http://example.test", "localhost"},
		{"north.example.com", "http://localhost:8090", "north.example.com"},
		{"", "http://north.example.com:8090/app", "north.example.com"},
	}
	for _, tc := range cases {
		if got := relyingPartyID(tc.in, tc.base); got != tc.want {
			t.Errorf("relyingPartyID(%q, %q) = %q, want %q", tc.in, tc.base, got, tc.want)
		}
	}
}
