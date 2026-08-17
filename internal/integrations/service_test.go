package integrations_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/integrations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/secret"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubCalendar stands in for the adapter, so service behaviour can be tested
// without a server.
type stubCalendar struct {
	lines []string
	err   error
	calls int
}

func (s *stubCalendar) Upcoming(_ context.Context, _, _ string, _ time.Time) ([]string, error) {
	s.calls++
	return s.lines, s.err
}

func serviceFixture(t *testing.T, cal integrations.Calendar) (*integrations.Service, users.User) {
	t.Helper()
	pool := testdb.New(t)

	// A real sealer: storing a token without one must fail, and that is part of
	// what these tests cover.
	keys, err := secret.ParseKeys("1:AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=")
	if err != nil {
		t.Fatalf("parse keys: %v", err)
	}
	sealer, err := secret.NewSealer(keys...)
	if err != nil {
		t.Fatalf("sealer: %v", err)
	}

	svc := integrations.NewService(integrations.NewRepository(pool, sealer), cal)

	userSvc := users.NewService(users.NewRepository(pool))
	u, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "calendar-" + uuid.NewString()[:8] + "@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, u
}

func TestConnectAndDisconnect(t *testing.T) {
	cal := &stubCalendar{lines: []string{"Tue 09:00 Standup"}}
	svc, u := serviceFixture(t, cal)
	ctx := context.Background()

	if _, ok, err := svc.Status(ctx, u.ID); err != nil || ok {
		t.Fatalf("a fresh account reported a connection: ok=%v err=%v", ok, err)
	}

	if err := svc.Connect(ctx, u.ID, "https://calendar.example.com/mcp", "tok"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	conn, ok, err := svc.Status(ctx, u.ID)
	if err != nil || !ok {
		t.Fatalf("status after connect: ok=%v err=%v", ok, err)
	}
	if conn.Endpoint != "https://calendar.example.com/mcp" || !conn.Healthy() {
		t.Fatalf("connection = %+v", conn)
	}

	if err := svc.Disconnect(ctx, u.ID); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, ok, _ := svc.Status(ctx, u.ID); ok {
		t.Fatal("still connected after disconnect")
	}
}

// Connecting proves the server answers, so a wrong address is caught now.
func TestConnectVerifiesTheServerAnswers(t *testing.T) {
	cal := &stubCalendar{err: errors.New("no route to host")}
	svc, u := serviceFixture(t, cal)
	ctx := context.Background()

	if err := svc.Connect(ctx, u.ID, "https://wrong.example.com/mcp", "tok"); err == nil {
		t.Fatal("connect accepted a server that did not answer")
	}

	// The row is kept, marked failed, so the page can say why rather than
	// making somebody retype a token to find out.
	conn, ok, err := svc.Status(ctx, u.ID)
	if err != nil || !ok {
		t.Fatalf("status: ok=%v err=%v", ok, err)
	}
	if conn.Healthy() {
		t.Fatal("a connection that did not answer is marked healthy")
	}
	if conn.LastError == "" {
		t.Fatal("no reason was recorded")
	}
}

// http to a public host would put the token on the wire in the clear.
func TestConnectRefusesPlaintextEndpoints(t *testing.T) {
	svc, u := serviceFixture(t, &stubCalendar{})
	ctx := context.Background()

	for _, endpoint := range []string{
		"http://calendar.example.com/mcp",
		"ftp://calendar.example.com",
		"not a url",
		"",
	} {
		if err := svc.Connect(ctx, u.ID, endpoint, "tok"); err == nil {
			t.Fatalf("accepted %q", endpoint)
		}
	}

	// Loopback over http stays allowed, for a locally-run server.
	if err := svc.Connect(ctx, u.ID, "http://localhost:9000/mcp", ""); err != nil {
		t.Fatalf("refused a loopback endpoint: %v", err)
	}
}

// Not being connected is the normal state, not a failure.
func TestUpcomingIsSilentWhenNothingIsConnected(t *testing.T) {
	cal := &stubCalendar{}
	svc, u := serviceFixture(t, cal)

	lines, err := svc.Upcoming(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("upcoming with no connection returned an error: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("lines = %v, want none", lines)
	}
	if cal.calls != 0 {
		t.Fatal("dialled a server with no connection stored")
	}
}

// The acceptance criterion: a failing calendar degrades the reply, never fails
// it. ContextBuilder is what enforces that, so this asserts the whole path.
func TestContextSourceFailureDegradesTheReply(t *testing.T) {
	cal := &stubCalendar{err: errors.New("calendar is down")}
	svc, u := serviceFixture(t, cal)
	ctx := context.Background()

	// Connect while the stub is healthy, then break it.
	cal.err = nil
	if err := svc.Connect(ctx, u.ID, "https://calendar.example.com/mcp", "tok"); err != nil {
		t.Fatal(err)
	}
	cal.err = errors.New("calendar is down")

	source := integrations.NewContextSource(svc)
	var built coach.Context
	err := source.Collect(ctx, coach.ContextRequest{User: u}, &built)
	if err == nil {
		t.Fatal("a broken calendar reported success; the builder would never count it")
	}
	if len(built.Calendar) != 0 {
		t.Fatal("a failing source wrote into the context anyway")
	}
	if source.Name() != "calendar" {
		t.Fatalf("name = %q, want calendar — it is what the metric is labelled with", source.Name())
	}
}

// A working calendar reaches the coach as plain strings.
func TestContextSourceFillsTheCalendarSection(t *testing.T) {
	cal := &stubCalendar{lines: []string{"Tue 09:00 Standup", "Wed 18:30 Squat session"}}
	svc, u := serviceFixture(t, cal)
	ctx := context.Background()

	if err := svc.Connect(ctx, u.ID, "https://calendar.example.com/mcp", "tok"); err != nil {
		t.Fatal(err)
	}

	var built coach.Context
	if err := integrations.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: u}, &built); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(built.Calendar) != 2 {
		t.Fatalf("calendar = %v", built.Calendar)
	}

	built.User = u
	if rendered := built.Render(); !strings.Contains(rendered, "Squat session") {
		t.Fatalf("the calendar did not reach the rendered context:\n%s", rendered)
	}
}
