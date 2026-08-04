// Package middleware holds the HTTP concerns that apply across every route:
// request identity, structured logging, panic recovery, and CSRF protection.
//
// Authentication middleware lives in internal/auth, next to the session code it
// depends on, so that this package stays free of business concepts.
package middleware

import "context"

// contextKey is unexported so no other package can write to this context
// namespace by accident.
type (
	contextKey      string
	ctxValue[T any] struct{ key contextKey }
)

func (k ctxValue[T]) set(ctx context.Context, v T) context.Context {
	return context.WithValue(ctx, k.key, v)
}

func (k ctxValue[T]) get(ctx context.Context) (T, bool) {
	v, ok := ctx.Value(k.key).(T)
	return v, ok
}
