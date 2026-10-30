package profiling

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheProfilesAreServed(t *testing.T) {
	h := Handler()
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap?debug=1", "/debug/pprof/goroutine?debug=1"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

func TestNothingElseIsServed(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/v1/me = %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "pprof") {
		t.Fatal("a path outside /debug/pprof answered with the profiler")
	}
}
