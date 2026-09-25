package apns_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/apns"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

const topic = "com.example.app"

// fakeSender answers every send with one result, or one error, and keeps
// what it was asked to deliver.
type fakeSender struct {
	mu       sync.Mutex
	result   apns.Result
	err      error
	payloads [][]byte
}

func (f *fakeSender) Send(_ context.Context, _ apns.Device, payload []byte) (apns.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, payload)
	return f.result, f.err
}

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func token(b string) string { return strings.Repeat(b, 32) }

func input(tok string) apns.Input {
	return apns.Input{Token: tok, Topic: topic, Environment: apns.EnvironmentProduction}
}

func newService(pool *pgxpool.Pool, sender apns.Sender) (*apns.Service, *apns.Repository) {
	repo := apns.NewRepository(pool)
	return apns.NewService(repo, sender, []string{topic}, nil), repo
}

func TestRegisterRefusesWhatIsNotOurDeviceToken(t *testing.T) {
	pool := testdb.New(t)
	svc, _ := newService(pool, &fakeSender{})
	u := seedUser(t, pool, "a@north.test")

	for name, tc := range map[string]struct {
		in    apns.Input
		field string
	}{
		"short token":  {apns.Input{Token: "abcd", Topic: topic, Environment: "production"}, "token"},
		"not hex":      {apns.Input{Token: strings.Repeat("zz", 32), Topic: topic, Environment: "production"}, "token"},
		"another app":  {apns.Input{Token: token("ab"), Topic: "com.other.app", Environment: "production"}, "topic"},
		"unknown host": {apns.Input{Token: token("ab"), Topic: topic, Environment: "staging"}, "environment"},
	} {
		_, err := svc.Register(t.Context(), u.ID, tc.in)
		var fe apperr.FieldErrors
		if !errors.As(err, &fe) || fe.Messages()[tc.field] == "" {
			t.Errorf("%s: err = %v, want a %s field error", name, err, tc.field)
		}
	}
}

func TestRegisterIsSwitchedOffWithoutAKey(t *testing.T) {
	pool := testdb.New(t)
	svc := apns.NewService(apns.NewRepository(pool), nil, nil, nil)
	u := seedUser(t, pool, "a@north.test")
	if _, err := svc.Register(t.Context(), u.ID, input(token("ab"))); !errors.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

// An install that signs in as somebody else takes its token with it, and an
// uppercase token is the same device as its lowercase form.
func TestATokenFollowsTheAccountThatRegisteredItLast(t *testing.T) {
	pool := testdb.New(t)
	svc, repo := newService(pool, &fakeSender{})
	ada := seedUser(t, pool, "ada@north.test")
	bob := seedUser(t, pool, "bob@north.test")

	if _, err := svc.Register(t.Context(), ada.ID, input(token("AB"))); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(t.Context(), bob.ID, input(token("ab"))); err != nil {
		t.Fatal(err)
	}

	adas, _ := repo.ListByUser(t.Context(), ada.ID)
	bobs, _ := repo.ListByUser(t.Context(), bob.ID)
	if len(adas) != 0 || len(bobs) != 1 {
		t.Fatalf("ada has %d, bob has %d; want the device to have moved to bob", len(adas), len(bobs))
	}
}

func TestSendDeliversTheNudgeWithItsLink(t *testing.T) {
	pool := testdb.New(t)
	sender := &fakeSender{result: apns.Result{Status: 200}}
	svc, _ := newService(pool, sender)
	u := seedUser(t, pool, "a@north.test")
	_, _ = svc.Register(t.Context(), u.ID, input(token("ab")))

	n, err := svc.Send(t.Context(), u.ID, "Check in", "How was today?", "/app/nudges/x/open?from=push")
	if err != nil || n != 1 {
		t.Fatalf("send = %d, %v", n, err)
	}

	var got struct {
		APS struct {
			Alert struct{ Title, Body string } `json:"alert"`
		} `json:"aps"`
		Href string `json:"href"`
	}
	if err := json.Unmarshal(sender.payloads[0], &got); err != nil {
		t.Fatal(err)
	}
	if got.APS.Alert.Title != "Check in" || got.APS.Alert.Body != "How was today?" || got.Href != "/app/nudges/x/open?from=push" {
		t.Fatalf("payload = %s", sender.payloads[0])
	}
}

func TestSendForgetsADeviceAppleSaysIsGone(t *testing.T) {
	pool := testdb.New(t)
	svc, repo := newService(pool, &fakeSender{result: apns.Result{Status: 410, Reason: "Unregistered"}})
	u := seedUser(t, pool, "a@north.test")
	_, _ = svc.Register(t.Context(), u.ID, input(token("ab")))

	if n, err := svc.Send(t.Context(), u.ID, "t", "b", ""); err != nil || n != 0 {
		t.Fatalf("send = %d, %v", n, err)
	}
	if left, _ := repo.ListByUser(t.Context(), u.ID); len(left) != 0 {
		t.Fatalf("devices left = %d, want the gone one deleted", len(left))
	}
}

func TestSendKeepsADeviceWhenAppleIsUnreachable(t *testing.T) {
	pool := testdb.New(t)
	svc, repo := newService(pool, &fakeSender{err: errors.New("connection reset")})
	u := seedUser(t, pool, "a@north.test")
	_, _ = svc.Register(t.Context(), u.ID, input(token("ab")))

	if n, err := svc.Send(t.Context(), u.ID, "t", "b", ""); err != nil || n != 0 {
		t.Fatalf("send = %d, %v", n, err)
	}
	left, _ := repo.ListByUser(t.Context(), u.ID)
	if len(left) != 1 || left[0].FailedAt == nil {
		t.Fatalf("devices = %+v, want one kept and marked failed", left)
	}
}
