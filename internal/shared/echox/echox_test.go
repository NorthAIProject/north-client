package echox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

func serve(t *testing.T, e *echo.Echo, method, path string, header http.Header) (*httptest.ResponseRecorder, httpx.ErrorBody) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var body httpx.ErrorBody
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
	}
	return rec, body
}

func newTestEcho() *echo.Echo {
	return New(Config{Logger: slog.New(slog.DiscardHandler)})
}

// The native app decodes {"error":{"message":…}} from every failure. The
// handler's own message must reach it with the status its error maps to.
func TestAFailureAnswersWithItsMessageAndMappedStatus(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	e.GET("/goals", func(*echo.Context) error {
		return httpx.Fail(apperr.FieldErrors{}.Add("title", "Give it a title."), "The goal could not be saved.")
	})

	rec, body := serve(t, e, http.MethodGet, "/goals", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if body.Error.Message != "The goal could not be saved." || body.Error.Fields["title"] != "Give it a title." {
		t.Fatalf("body = %+v", body)
	}
}

// An unwrapped internal error must never reach a client in its own words.
func TestAPlainErrorIsAGeneric500(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	e.GET("/boom", func(*echo.Context) error { return errors.New("pq: relation users does not exist") })

	rec, body := serve(t, e, http.MethodGet, "/boom", nil)
	if rec.Code != http.StatusInternalServerError || body.Error.Message != "Something went wrong." {
		t.Fatalf("got %d %+v", rec.Code, body)
	}
}

func TestAPanicIsRecoveredAsA500(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	e.GET("/panic", func(*echo.Context) error { panic("nil map") })

	rec, body := serve(t, e, http.MethodGet, "/panic", nil)
	if rec.Code != http.StatusInternalServerError || body.Error.Message != "Something went wrong." {
		t.Fatalf("got %d %+v", rec.Code, body)
	}
}

func TestAnUnknownRouteIsAJSON404(t *testing.T) {
	t.Parallel()
	e := newTestEcho()

	rec, body := serve(t, e, http.MethodGet, "/nowhere", nil)
	if rec.Code != http.StatusNotFound || body.Error.Message == "" {
		t.Fatalf("got %d %+v", rec.Code, body)
	}
}

// A group's middleware must not claim its prefix: the fallback registered on
// Echo has to see paths the group never routed.
func TestAGroupWithMiddlewareDoesNotShadowTheFallback(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	g := e.Group("/api/v1", func(next echo.HandlerFunc) echo.HandlerFunc { return next })
	g.GET("/goals", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	e.RouteNotFound("/*", func(c *echo.Context) error { return c.String(http.StatusTeapot, "fallback") })

	if rec, _ := serve(t, e, http.MethodGet, "/api/v1/legacy", nil); rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the fallback", rec.Code)
	}
}

func TestARequestIDIsHonouredEchoedAndOnTheContext(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	var seen string
	e.GET("/id", func(c *echo.Context) error {
		seen = middleware.RequestIDFrom(c.Request().Context())
		return c.NoContent(http.StatusOK)
	})

	rec, _ := serve(t, e, http.MethodGet, "/id", http.Header{"X-Request-Id": {"trace-1"}})
	if seen != "trace-1" || rec.Header().Get(echo.HeaderXRequestID) != "trace-1" {
		t.Fatalf("context %q, header %q", seen, rec.Header().Get(echo.HeaderXRequestID))
	}

	long := strings.Repeat("x", maxRequestIDLength+1)
	rec, _ = serve(t, e, http.MethodGet, "/id", http.Header{"X-Request-Id": {long}})
	if got := rec.Header().Get(echo.HeaderXRequestID); got == long || got == "" {
		t.Fatalf("an oversized id should be replaced, got %q", got)
	}
}

func TestServeShutsDownWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, newTestEcho(), ServerConfig{Addr: "127.0.0.1:0", Logger: slog.New(slog.DiscardHandler)})
	}()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v, want a clean shutdown", err)
	}
}

// Coach replies and crew chat stream over SSE through c.Response(). A flush
// that stopped at Echo's writer would turn every stream into one buffered
// blob at the end.
func TestAFlushThroughTheResponseReachesTheConnection(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	e.GET("/stream", func(c *echo.Context) error {
		c.Response().Header().Set("Content-Type", "text/event-stream")
		c.Response().WriteHeader(http.StatusOK)
		_, _ = c.Response().Write([]byte("event: token\ndata: {}\n\n"))
		return http.NewResponseController(c.Response()).Flush()
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if !rec.Flushed {
		t.Fatal("the flush did not reach the underlying writer")
	}
}

func TestEveryReplyCarriesTheHardeningHeaders(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	e.GET("/ok", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	// HSTS is sent only over HTTPS. Behind the ingress that is known from
	// X-Forwarded-Proto, which Echo believes only from a private peer.
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "10.42.0.7:4000"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	for header, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Strict-Transport-Security": "max-age=31536000; includeSubdomains; preload",
		"X-Xss-Protection":          "",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

// RealIP must believe only the configured proxies. Echo's own default trusts
// every private address, which on a cluster network is every pod.
func TestRealIPBelievesOnlyTheConfiguredProxies(t *testing.T) {
	t.Parallel()
	trusted, err := middleware.ParseTrustedProxies("10.42.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	e := New(Config{Logger: slog.New(slog.DiscardHandler), TrustedProxies: trusted})
	var seen string
	e.GET("/ip", func(c *echo.Context) error {
		seen = c.RealIP()
		return c.NoContent(http.StatusOK)
	})

	for peer, want := range map[string]string{
		"10.42.0.7:4000":   "198.51.100.1",
		"192.168.1.5:4000": "192.168.1.5",
	} {
		req := httptest.NewRequest(http.MethodGet, "/ip", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", "198.51.100.1")
		e.ServeHTTP(httptest.NewRecorder(), req)
		if seen != want {
			t.Errorf("peer %s: RealIP = %q, want %q", peer, seen, want)
		}
	}
}

func TestGzipCompressesLargeRepliesButNeverAStream(t *testing.T) {
	t.Parallel()
	e := newTestEcho()
	g := e.Group("/api", Gzip("/api/stream"))
	big := strings.Repeat(`{"k":"v"},`, gzipMinLength)
	g.GET("/list", func(c *echo.Context) error { return c.String(http.StatusOK, big) })
	g.GET("/small", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })
	g.GET("/stream", func(c *echo.Context) error { return c.String(http.StatusOK, big) })

	gzipped := func(path string, header http.Header) bool {
		header = header.Clone()
		header.Set("Accept-Encoding", "gzip")
		rec, _ := serve(t, e, http.MethodGet, path, header)
		return rec.Header().Get("Content-Encoding") == "gzip"
	}

	if !gzipped("/api/list", http.Header{}) {
		t.Error("a large reply was not compressed")
	}
	if gzipped("/api/small", http.Header{}) {
		t.Error("a reply under the minimum was compressed")
	}
	if gzipped("/api/stream", http.Header{}) {
		t.Error("a stream route was compressed")
	}
	if gzipped("/api/list", http.Header{"Accept": {"text/event-stream"}}) {
		t.Error("a request for an event stream was compressed")
	}
}
