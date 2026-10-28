// Package echox is the Echo edge of the server: the instance every route is
// registered on, the middleware every request passes through, the one place
// an error returned by a handler becomes a reply, and the server lifecycle.
//
// Feature packages keep their handlers and register them on an *echo.Group
// the composition root hands them. They return errors rather than writing
// them: httpx.Fail attaches the message a client should read, and
// ErrorHandler turns it into the JSON shape httpx.Error has always written,
// so the native app sees the same bodies it decoded before Echo.
package echox

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
	echomw "github.com/labstack/echo/v5/middleware"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Config is what New needs from the process.
type Config struct {
	Logger *slog.Logger
}

// New builds the Echo instance with the middleware every request runs
// through, in this order:
//
//   - RequestID first, so every later line can carry it;
//   - the request logger, outside Recover so a panic is logged with the 500
//     it became;
//   - Recover, so one bad handler cannot take the process down.
//
// Groups do not register catch-all 404 routes for their middleware. Without
// that, every group with a body limit or an auth check would claim its whole
// prefix and shadow the RouteNotFound fallbacks the composition root sets.
func New(cfg Config) *echo.Echo {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	e := echo.NewWithConfig(echo.Config{
		Logger:                       log,
		HTTPErrorHandler:             ErrorHandler,
		NoGroupAutoRegister404Routes: true,
	})

	e.Use(RequestID())
	e.Use(RequestLogger(log))
	e.Use(echomw.Recover())

	return e
}

// maxRequestIDLength bounds an inbound X-Request-ID. Anything longer is not a
// trace id a proxy minted; it is somebody filling the logs.
const maxRequestIDLength = 200

// RequestID honours an inbound X-Request-ID, mints a UUID when there is none,
// echoes it back, and puts it on the request context, where
// middleware.RequestIDFrom and the jobs the request queues find it.
func RequestID() echo.MiddlewareFunc {
	stamp := echomw.RequestIDWithConfig(echomw.RequestIDConfig{
		Generator: uuid.NewString,
		RequestIDHandler: func(c *echo.Context, id string) {
			r := c.Request()
			c.SetRequest(r.WithContext(middleware.WithRequestID(r.Context(), id)))
		},
	})

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		stamped := stamp(next)
		return func(c *echo.Context) error {
			if len(c.Request().Header.Get(echo.HeaderXRequestID)) > maxRequestIDLength {
				c.Request().Header.Del(echo.HeaderXRequestID)
			}
			return stamped(c)
		}
	}
}

// RequestLogger writes one structured line per request and hangs a logger
// carrying the request id on the context, where middleware.FromContext finds
// it for every line a handler writes.
//
// Server errors are logged at error level, everything else at info: a 5xx is
// the one worth waking up for.
func RequestLogger(base *slog.Logger) echo.MiddlewareFunc {
	return echomw.RequestLoggerWithConfig(echomw.RequestLoggerConfig{
		HandleError:     true,
		LogStatus:       true,
		LogMethod:       true,
		LogURIPath:      true,
		LogRequestID:    true,
		LogLatency:      true,
		LogResponseSize: true,
		BeforeNextFunc: func(c *echo.Context) {
			r := c.Request()
			log := base.With(
				slog.String("request_id", middleware.RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			c.SetRequest(r.WithContext(middleware.WithLogger(r.Context(), log)))
		},
		LogValuesFunc: func(c *echo.Context, v echomw.RequestLoggerValues) error {
			level := slog.LevelInfo
			if v.Status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			middleware.FromContext(c.Request().Context()).LogAttrs(c.Request().Context(), level, "request",
				slog.Int("status", v.Status),
				slog.Int64("bytes", v.ResponseSize),
				slog.Duration("duration", v.Latency),
			)
			return nil
		},
	})
}

// ErrorHandler answers every error a handler or middleware returns, in the
// one JSON shape httpx.Error writes.
//
//   - httpx.Failure carries the endpoint's own message;
//   - an echo.HTTPStatusCoder is Echo's own refusal (no route, wrong method, a
//     body over its limit) and speaks with its status text;
//   - anything else is mapped by httpx.Status and given a generic message,
//     so a wrapped internal error never reaches a client.
//
// Server errors are logged with the request's logger. A reply already under
// way cannot be replaced, so an error after that point is only logged.
func ErrorHandler(c *echo.Context, err error) {
	ctx := c.Request().Context()
	if resp, _ := echo.UnwrapResponse(c.Response()); resp != nil && resp.Committed {
		if !errors.Is(err, context.Canceled) {
			middleware.FromContext(ctx).Warn("error after the reply started", slog.Any("error", err))
		}
		return
	}

	var (
		cause   = err
		message = "Something went wrong."
	)

	var failure *httpx.Failure
	var coded echo.HTTPStatusCoder
	switch {
	case errors.As(err, &failure):
		cause, message = failure.Err, failure.Message
	case errors.As(err, &coded) && coded.StatusCode() != 0:
		var echoErr *echo.HTTPError
		if errors.As(err, &echoErr) {
			writeStatus(c, coded.StatusCode(), echoErr.Message)
		} else {
			writeStatus(c, coded.StatusCode(), "")
		}
		return
	}

	if status := httpx.Status(cause); status >= http.StatusInternalServerError {
		var panicked *echomw.PanicStackError
		if errors.As(err, &panicked) {
			middleware.FromContext(ctx).Error("panic recovered",
				slog.Any("panic", panicked.Err),
				slog.String("stack", string(panicked.Stack)),
			)
		} else {
			middleware.FromContext(ctx).Error("request failed", slog.Any("error", err))
		}
	}

	if c.Request().Method == http.MethodHead {
		_ = c.NoContent(httpx.Status(cause))
		return
	}
	httpx.Error(c.Response(), cause, message)
}

// writeStatus answers one of Echo's own refusals.
func writeStatus(c *echo.Context, code int, message string) {
	if message == "" {
		message = http.StatusText(code)
	}
	if c.Request().Method == http.MethodHead {
		_ = c.NoContent(code)
		return
	}
	httpx.WriteJSON(c.Response(), code, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: message}})
}

// BindJSON decodes a JSON body with httpx.ReadJSON's strictness, failing with
// message when the body is not what the endpoint wanted.
func BindJSON(c *echo.Context, dst any, opts httpx.ReadOptions, message string) error {
	if err := httpx.ReadJSON(c.Response(), c.Request(), dst, opts); err != nil {
		return httpx.Fail(err, message)
	}
	return nil
}

// UUIDParam reads a path parameter that names a row. One that does not parse
// names nothing, so it is a 404 rather than a validation error: the caller
// asked for something that does not exist.
func UUIDParam(c *echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, httpx.Fail(apperr.ErrNotFound, "Not found.")
	}
	return id, nil
}

// ServerConfig is how the process serves.
type ServerConfig struct {
	Addr string
	// GracefulTimeout bounds how long shutdown waits for requests in flight.
	GracefulTimeout time.Duration
	Logger          *slog.Logger
}

// Serve runs h until ctx is cancelled, then shuts down gracefully: the
// listener closes, requests in flight get GracefulTimeout to finish, and
// Serve returns once they have.
//
// The timeouts are North's, not Echo's defaults. Echo sets a 30-second
// ReadTimeout, which would cut off a form-check video on a slow phone
// connection; only the header read is bounded here. There is no
// WriteTimeout, because coach replies and crew chat stream over SSE for as
// long as the conversation lasts.
func Serve(ctx context.Context, h http.Handler, cfg ServerConfig) error {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	sc := echo.StartConfig{
		Address:         cfg.Addr,
		HideBanner:      true,
		HidePort:        true,
		GracefulTimeout: cfg.GracefulTimeout,
		ListenerAddrFunc: func(addr net.Addr) {
			log.Info("server listening", slog.String("addr", addr.String()))
		},
		OnShutdownError: func(err error) {
			log.Error("graceful shutdown did not finish", slog.Any("error", err))
		},
		BeforeServeFunc: func(s *http.Server) error {
			s.ReadTimeout = 0
			s.ReadHeaderTimeout = 10 * time.Second
			s.IdleTimeout = 120 * time.Second
			return nil
		},
	}
	return sc.Start(ctx, h)
}
