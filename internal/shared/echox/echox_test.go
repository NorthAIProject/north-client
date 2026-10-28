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
