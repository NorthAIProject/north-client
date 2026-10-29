package moderation_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/moderation"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func person(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: strings.ToLower(name) + "@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: name, Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// hider stands in for a slice that owns reportable content.
type hider struct {
	mu        sync.Mutex
	hidden    []uuid.UUID
	unhidden  []uuid.UUID
	threshold int
}

func (h *hider) Hide(_ context.Context, id uuid.UUID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hidden = append(h.hidden, id)
	return nil
}

func (h *hider) Unhide(_ context.Context, id uuid.UUID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unhidden = append(h.unhidden, id)
	return nil
}

func (h *hider) Threshold(context.Context, uuid.UUID) (int, error) { return h.threshold, nil }

func (h *hider) Excerpt(context.Context, uuid.UUID) (string, error) { return "the words", nil }

// pusher records what operators were sent.
type pusher struct {
	mu   sync.Mutex
	sent []uuid.UUID
}

func (p *pusher) SendThread(_ context.Context, userID uuid.UUID, _, _, _, _, _ string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, userID)
	return 1, nil
}

func fieldError(err error, field string) string {
	var fe apperr.FieldErrors
	if !errors.As(err, &fe) {
		return ""
	}
	return fe.Messages()[field]
}

func setup(t *testing.T) (*pgxpool.Pool, *moderation.Service, *hider) {
	t.Helper()
	pool := testdb.New(t)
	svc := moderation.NewService(moderation.NewRepository(pool))
	h := &hider{threshold: 3}
	svc.Register("crew_message", h)
	return pool, svc, h
}

// The third distinct reporter hides the item, once, and the hide is on the
// record as automatic.
func TestReportAutoHidesAtThreshold(t *testing.T) {
	pool, svc, h := setup(t)
	ctx := context.Background()
	author := person(t, pool, "Author")
	sub := moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: author}

	for i := 1; i <= 4; i++ {
		hidden, err := svc.Report(ctx, person(t, pool, fmt.Sprintf("R%d", i)), sub, "harassment", "")
		if err != nil {
			t.Fatal(err)
		}
		if hidden != (i == 3) {
			t.Fatalf("report %d hidden = %v", i, hidden)
		}
	}
	if len(h.hidden) != 1 || h.hidden[0] != sub.ID {
		t.Fatalf("hidden = %v; want once, %v", h.hidden, sub.ID)
	}
	var actions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM moderation_actions
		WHERE subject_id = $1 AND action = 'hide' AND actor_id IS NULL`, sub.ID).Scan(&actions); err != nil {
		t.Fatal(err)
	}
	if actions != 1 {
		t.Fatalf("%d automatic hide actions; want 1", actions)
	}
}

func TestReportTwiceIsNoop(t *testing.T) {
	pool, svc, _ := setup(t)
	ctx := context.Background()
	reporter := person(t, pool, "Reporter")
	sub := moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: person(t, pool, "Author")}

	for range 2 {
		if _, err := svc.Report(ctx, reporter, sub, "spam", ""); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM content_reports WHERE subject_id = $1`, sub.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d reports; want 1", n)
	}
}

func TestReportOwnContentRefused(t *testing.T) {
	pool, svc, _ := setup(t)
	author := person(t, pool, "Author")
	_, err := svc.Report(context.Background(), author, moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: author}, "spam", "")
	if fieldError(err, "report") == "" {
		t.Fatalf("reported own content: %v", err)
	}
}

func TestReportUnknownKindOrReason(t *testing.T) {
	pool, svc, _ := setup(t)
	ctx := context.Background()
	reporter := person(t, pool, "Reporter")
	author := person(t, pool, "Author")

	if _, err := svc.Report(ctx, reporter, moderation.Subject{Kind: "nope", ID: uuid.New(), AuthorID: author}, "spam", ""); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("unknown kind: %v", err)
	}
	sub := moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: author}
	if _, err := svc.Report(ctx, reporter, sub, "meh", ""); fieldError(err, "reason") == "" {
		t.Fatalf("unknown reason: %v", err)
	}
	if _, err := svc.Report(ctx, reporter, sub, "other", strings.Repeat("é", 501)); fieldError(err, "note") == "" {
		t.Fatalf("501-character note: %v", err)
	}
}

// What somebody reported disappears for them straight away.
func TestReportedBy(t *testing.T) {
	pool, svc, _ := setup(t)
	ctx := context.Background()
	ana := person(t, pool, "Ana")
	leo := person(t, pool, "Leo")
	author := person(t, pool, "Author")
	mine, theirs, untouched := uuid.New(), uuid.New(), uuid.New()
	if _, err := svc.Report(ctx, ana, moderation.Subject{Kind: "crew_message", ID: mine, AuthorID: author}, "spam", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Report(ctx, leo, moderation.Subject{Kind: "crew_message", ID: theirs, AuthorID: author}, "spam", ""); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ReportedBy(ctx, ana, "crew_message", []uuid.UUID{mine, theirs, untouched})
	if err != nil {
		t.Fatal(err)
	}
	if !got[mine] || got[theirs] || got[untouched] {
		t.Fatalf("reported by ana = %v", got)
	}
}

// Posting needs the terms accepted and no suspension running.
func TestCanPost(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := moderation.NewService(moderation.NewRepository(pool)).WithClock(func() time.Time { return now })
	ana := person(t, pool, "Ana")
	op := person(t, pool, "Operator")

	if err := svc.CanPost(ctx, ana); !errors.Is(err, moderation.ErrTermsRequired) {
		t.Fatalf("before terms: %v", err)
	}
	if err := svc.AcceptTerms(ctx, ana); err != nil {
		t.Fatal(err)
	}
	if err := svc.CanPost(ctx, ana); err != nil {
		t.Fatalf("after terms: %v", err)
	}

	until := now.Add(time.Hour)
	if err := svc.Suspend(ctx, &op, ana, &until, "spam run"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CanPost(ctx, ana); !errors.Is(err, moderation.ErrSuspended) {
		t.Fatalf("while suspended: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if err := svc.CanPost(ctx, ana); err != nil {
		t.Fatalf("after the suspension ran out: %v", err)
	}

	if err := svc.Suspend(ctx, &op, ana, nil, "again"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CanPost(ctx, ana); !errors.Is(err, moderation.ErrSuspended) {
		t.Fatalf("open-ended suspension: %v", err)
	}
	if err := svc.Unsuspend(ctx, &op, ana); err != nil {
		t.Fatal(err)
	}
	if err := svc.CanPost(ctx, ana); err != nil {
		t.Fatalf("after unsuspend: %v", err)
	}
}

// A threshold that drops after enough reports are already in (a crew that
// shrank) still hides on the next report, and a report with no author (a
// coach message) is accepted.
func TestReportHidesWhenThresholdDropsBelowCount(t *testing.T) {
	pool, svc, h := setup(t)
	ctx := context.Background()
	sub := moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: person(t, pool, "Author")}
	for i := 1; i <= 2; i++ {
		if _, err := svc.Report(ctx, person(t, pool, fmt.Sprintf("R%d", i)), sub, "spam", ""); err != nil {
			t.Fatal(err)
		}
	}
	h.threshold = 2
	hidden, err := svc.Report(ctx, person(t, pool, "R3"), sub, "spam", "")
	if err != nil || !hidden || len(h.hidden) != 1 {
		t.Fatalf("third report after the threshold fell to 2: hidden %v, hides %d, %v", hidden, len(h.hidden), err)
	}
	if hidden, err = svc.Report(ctx, person(t, pool, "R4"), sub, "spam", ""); err != nil || hidden || len(h.hidden) != 1 {
		t.Fatalf("fourth report hid again: hidden %v, hides %d, %v", hidden, len(h.hidden), err)
	}
}

func TestReportWithoutAuthor(t *testing.T) {
	pool, svc, _ := setup(t)
	sub := moderation.Subject{Kind: "crew_message", ID: uuid.New()}
	if _, err := svc.Report(context.Background(), person(t, pool, "Reporter"), sub, "other", ""); err != nil {
		t.Fatalf("report of an authorless subject: %v", err)
	}
}

// An operator's call closes the reports: hide or unhide goes through the
// slice and on the record; dismiss only closes them. Non-operators are not
// let in.
func TestDecide(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	op := person(t, pool, "Operator")
	svc := moderation.NewService(moderation.NewRepository(pool)).WithOperators([]uuid.UUID{op}, nil)
	h := &hider{threshold: 3}
	svc.Register("crew_message", h)
	author := person(t, pool, "Author")
	a := moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: author}
	b := moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: author}
	for _, sub := range []moderation.Subject{a, b} {
		if _, err := svc.Report(ctx, person(t, pool, "R"+sub.ID.String()[:4]), sub, "spam", ""); err != nil {
			t.Fatal(err)
		}
	}

	open, err := svc.OpenReports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 || open[0].Count != 1 || open[0].Reasons["spam"] != 1 || open[0].Excerpt != "the words" {
		t.Fatalf("open = %+v", open)
	}

	if err = svc.Decide(ctx, author, "crew_message", a.ID, "hide"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a non-operator deciding: %v", err)
	}
	if err = svc.Decide(ctx, op, "crew_message", a.ID, "hide"); err != nil {
		t.Fatal(err)
	}
	if err = svc.Decide(ctx, op, "crew_message", a.ID, "unhide"); err != nil {
		t.Fatal(err)
	}
	if err = svc.Decide(ctx, op, "crew_message", b.ID, "dismiss"); err != nil {
		t.Fatal(err)
	}
	if err = svc.Decide(ctx, op, "crew_message", b.ID, "explode"); fieldError(err, "action") == "" {
		t.Fatalf("an unknown action: %v", err)
	}
	if len(h.hidden) != 1 || len(h.unhidden) != 1 {
		t.Fatalf("hidden %v, unhidden %v", h.hidden, h.unhidden)
	}
	if open, err = svc.OpenReports(ctx); err != nil || len(open) != 0 {
		t.Fatalf("after deciding both: %+v, %v", open, err)
	}
}

// Operators hear about every new report, once.
func TestOperatorPushedOnReport(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ops := []uuid.UUID{person(t, pool, "Op1"), person(t, pool, "Op2")}
	p := &pusher{}
	svc := moderation.NewService(moderation.NewRepository(pool)).WithOperators(ops, p)
	svc.Register("crew_message", &hider{threshold: 3})
	sub := moderation.Subject{Kind: "crew_message", ID: uuid.New(), AuthorID: person(t, pool, "Author")}
	reporter := person(t, pool, "Reporter")
	for range 2 {
		if _, err := svc.Report(ctx, reporter, sub, "hate", ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.sent) != 2 {
		t.Fatalf("operators pushed %d times; want once each", len(p.sent))
	}
}
