package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v5"
)

// metricsHandler returns the total user count for the portfolio dashboard at
// facorreia.com/apps. It is guarded by METRICS_SECRET; callers must present
// the secret as a Bearer token.
func metricsHandler(pool *pgxpool.Pool, secret string) echo.HandlerFunc {
	return func(c *echo.Context) error {
		r, w := c.Request(), c.Response()
		presented, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !found || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return nil
		}
		var count int64
		if err := pool.QueryRow(r.Context(), "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return nil
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"users":%d}`, count) //nolint:errcheck
		return nil
	}
}
