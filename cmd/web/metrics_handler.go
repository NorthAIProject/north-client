package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// metricsHandler returns the total user count for the portfolio dashboard at
// facorreia.com/apps. It is guarded by METRICS_SECRET; callers must present
// the secret as a Bearer token.
func metricsHandler(pool *pgxpool.Pool, secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presented, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !found || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(presented)), []byte(secret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var count int64
		if err := pool.QueryRow(r.Context(), "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"users":%d}`, count) //nolint:errcheck
	}
}
