package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/config"
)

// These cover the one asset tree that is not served as it is stored. The
// frames live on disk as .svg.gz and templates ask for .svg, so a browser only
// ever gets a usable image if the rewrite and the Content-Encoding header stay
// in step. Getting either half wrong renders a broken image rather than
// failing anything, which is why it is worth a test.
//
// No database: mountAssets is mounted directly rather than through routes().

func assetRouter(t *testing.T, env config.Environment) http.Handler {
	t.Helper()

	r := chi.NewRouter()
	mountAssets(r, &config.Config{Env: env})
	return r
}

func TestAnExerciseFrameIsServedGzippedAndDecompressesToSVG(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/assets/exercises/bench-press/frame-1.svg", nil)
	rec := httptest.NewRecorder()
	assetRouter(t, config.EnvProduction).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — is web/assets/exercises embedded?", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml (the path ends .gz, so this has to be set explicitly)", got)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not gzip despite the header saying so: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompressing the frame: %v", err)
	}
	if !strings.HasPrefix(string(body), "<svg") {
		t.Errorf("decompressed body starts %.40q, want an <svg element", body)
	}
}

// Every exercise ships three frames. A component that renders frame-3 against
// a set that only stored two would show an empty box.
func TestEveryFrameIndexTheComponentRendersExists(t *testing.T) {
	t.Parallel()

	router := assetRouter(t, config.EnvProduction)
	for _, frame := range []string{"frame-1.svg", "frame-2.svg", "frame-3.svg"} {
		req := httptest.NewRequest(http.MethodGet, "/assets/exercises/bench-press/"+frame, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", frame, rec.Code)
		}
	}
}

// The rewrite is scoped to the exercises tree. Claiming gzip on an asset that
// is stored plain would hand the browser bytes it cannot decode.
func TestOtherAssetsAreNotClaimedToBeGzipped(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/assets/brand/favicon.svg", nil)
	rec := httptest.NewRecorder()
	assetRouter(t, config.EnvProduction).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q on a plain asset, want it unset", got)
	}
}

// A slug with no artwork must 404 rather than serve something. The catalog
// carries more movements than the artwork covers, so this is a normal request,
// not a malformed one.
func TestAnUnknownExerciseFrameIsNotFound(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/assets/exercises/not-an-exercise/frame-1.svg", nil)
	rec := httptest.NewRecorder()
	assetRouter(t, config.EnvProduction).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
