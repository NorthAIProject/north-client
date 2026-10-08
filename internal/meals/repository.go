package meals

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
)

// Repository is shared by all seven meal services. One struct rather than
// one per service: they all read and write the same eight tables, and
// splitting the struct would just mean passing the same *pgxpool.Pool-backed
// queries object under seven different names.
type Repository struct {
	q    *mealsdb.Queries
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: mealsdb.New(pool), pool: pool}
}

// macrosToJSON/macrosFromJSON convert between Macros and the jsonb columns
// used to cache meal/plan totals. A parse failure on read is treated as
// empty rather than propagated: a cache that fails to decode should recompute
// as empty, not break the page.
func macrosToJSON(m Macros) []byte {
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func macrosFromJSON(b []byte) Macros {
	var m Macros
	if len(b) == 0 {
		return m
	}
	_ = json.Unmarshal(b, &m)
	return m
}

func toDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func fromDate(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return d.Time
}

func int16sToInts(in []int16) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

func intsToInt16s(in []int) []int16 {
	out := make([]int16, len(in))
	for i, v := range in {
		out[i] = int16(v)
	}
	return out
}
