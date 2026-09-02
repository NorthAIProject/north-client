package push_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/push"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// fakeSender answers every send with one status, or one error, and keeps what
// it was asked to deliver. It is the push service on the other end of the wire.
type fakeSender struct {
	mu       sync.Mutex
	status   int
	err      error
	payloads [][]byte
	to       []string
}

func (f *fakeSender) Send(_ context.Context, sub push.Subscription, payload []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payloads = append(f.payloads, payload)
	f.to = append(f.to, sub.Endpoint)
	if f.err != nil {
		return 0, f.err
	}
	return f.status, nil
}

type fakeFunnel struct {
	subscribed []uuid.UUID
}

func (f *fakeFunnel) PushSubscribed(_ context.Context, userID uuid.UUID) {
	f.subscribed = append(f.subscribed, userID)
}

var testVAPID = push.VAPID{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:ops@north.test"}

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

func validInput(endpoint string) push.Input {
	return push.Input{
		Endpoint:  endpoint,
		P256dh:    "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM",
		Auth:      "tBHItJI5svbpez7KI4CCXg",
		UserAgent: "Mozilla/5.0 (iPhone)",
	}
}

func newService(pool *pgxpool.Pool, sender push.Sender) *push.Service {
	return push.NewService(push.NewRepository(pool), sender, testVAPID, nil)
}

func TestSubscribeStoresOneRowPerEndpoint(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "push-subscribe@north.test")
	funnel := &fakeFunnel{}
	svc := newService(pool, &fakeSender{status: 201}).WithFunnel(funnel)

	if _, err := svc.Subscribe(ctx, user.ID, validInput("https://push.example/a")); err != nil {
		t.Fatal(err)
	}
	// The same browser again, with rotated keys. One row, new keys.
	again := validInput("https://push.example/a")
	again.Auth = "rotated-auth-secret"
	if _, err := svc.Subscribe(ctx, user.ID, again); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Subscribe(ctx, user.ID, validInput("https://push.example/b")); err != nil {
		t.Fatal(err)
	}

	n, err := svc.Count(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("count = %d, want 2 (two endpoints, one re-subscribed)", n)
	}

	subs, err := push.NewRepository(pool).ListByUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range subs {
		if s.Endpoint == "https://push.example/a" && s.Auth != "rotated-auth-secret" {
			t.Errorf("re-subscribe kept the old auth secret %q", s.Auth)
		}
	}

	if len(funnel.subscribed) != 3 {
		t.Errorf("funnel saw %d opt-ins, want 3", len(funnel.subscribed))
	}

	has, err := svc.HasSubscription(ctx, user.ID)
	if err != nil || !has {
		t.Fatalf("HasSubscription = %v, %v; want true", has, err)
	}
}

func TestSubscribeRefusesWhatIsNotASubscription(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "push-validate@north.test")
	svc := newService(pool, &fakeSender{status: 201})

	cases := map[string]push.Input{
		"http endpoint": func() push.Input { in := validInput("http://push.example/a"); return in }(),
		"no endpoint":   func() push.Input { in := validInput(""); return in }(),
		"no p256dh":     func() push.Input { in := validInput("https://push.example/a"); in.P256dh = ""; return in }(),
		"no auth":       func() push.Input { in := validInput("https://push.example/a"); in.Auth = " "; return in }(),
		"endpoint too long": func() push.Input {
			return validInput("https://push.example/" + strings.Repeat("x", 3000))
		}(),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Subscribe(ctx, user.ID, in)
			if !apperr.Is(err, apperr.ErrValidation) {
				t.Fatalf("err = %v, want validation", err)
			}
		})
	}

	n, _ := svc.Count(ctx, user.ID)
	if n != 0 {
		t.Fatalf("count = %d after refused subscriptions, want 0", n)
	}
}

// A deployment without VAPID keys must be inert in every direction: it
// offers nothing, accepts nothing, and Send is a no-op that reports zero.
func TestAServiceWithoutKeysIsOff(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "push-off@north.test")

	svc := push.NewService(push.NewRepository(pool), push.NewSender(push.VAPID{}), push.VAPID{}, nil)
	if svc.Enabled() {
		t.Fatal("Enabled with no keys")
	}
	if _, err := svc.Subscribe(ctx, user.ID, validInput("https://push.example/a")); !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("Subscribe err = %v, want unavailable", err)
	}
	delivered, err := svc.Send(ctx, user.ID, "Hi", "there", "/app")
	if err != nil || delivered != 0 {
		t.Fatalf("Send = %d, %v; want 0, nil", delivered, err)
	}
	if svc.PublicKey() != "" {
		t.Fatal("a service without keys has a public key")
	}
}

func TestSendDeliversToEveryBrowserAndRecordsIt(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "push-send@north.test")
	sender := &fakeSender{status: 201}
	svc := newService(pool, sender)

	for _, ep := range []string{"https://push.example/phone", "https://push.example/laptop"} {
		if _, err := svc.Subscribe(ctx, user.ID, validInput(ep)); err != nil {
			t.Fatal(err)
		}
	}

	delivered, err := svc.Send(ctx, user.ID, "Check in with yourself", "It has been 3 days.", "/app/nudges/x/open?from=push")
	if err != nil {
		t.Fatal(err)
	}
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2", delivered)
	}
	if len(sender.payloads) != 2 {
		t.Fatalf("sender got %d payloads, want 2", len(sender.payloads))
	}

	var msg struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Href  string `json:"href"`
	}
	if decodeErr := json.Unmarshal(sender.payloads[0], &msg); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if msg.Title != "Check in with yourself" || msg.Body != "It has been 3 days." || msg.Href != "/app/nudges/x/open?from=push" {
		t.Fatalf("payload = %+v", msg)
	}

	subs, err := push.NewRepository(pool).ListByUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range subs {
		if s.LastUsedAt == nil {
			t.Errorf("%s: accepted send did not stamp last_used_at", s.Endpoint)
		}
		if s.FailedAt != nil {
			t.Errorf("%s: accepted send stamped failed_at", s.Endpoint)
		}
	}
}

func TestSendClipsALongBody(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "push-clip@north.test")
	sender := &fakeSender{status: 201}
	svc := newService(pool, sender)
	if _, err := svc.Subscribe(ctx, user.ID, validInput("https://push.example/a")); err != nil {
		t.Fatal(err)
	}

	long := strings.Repeat("é", 900)
	if _, err := svc.Send(ctx, user.ID, "t", long, "/app"); err != nil {
		t.Fatal(err)
	}
	var msg struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(sender.payloads[0], &msg); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(msg.Body)); n != 500 {
		t.Fatalf("body is %d runes, want 500", n)
	}
	if !strings.HasSuffix(msg.Body, "…") {
		t.Fatal("clipped body does not end in an ellipsis")
	}
}

// 404 and 410 are the push service saying the browser is gone for good. The
// row goes with it, so the next sweep does not knock on the same closed door.
func TestSendForgetsASubscriptionThePushServiceDeclaresGone(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "push-gone@north.test")

	for _, status := range []int{404, 410} {
		sender := &fakeSender{status: status}
		svc := newService(pool, sender)
		if _, err := svc.Subscribe(ctx, user.ID, validInput("https://push.example/gone")); err != nil {
			t.Fatal(err)
		}

		delivered, err := svc.Send(ctx, user.ID, "t", "b", "/app")
		if err != nil || delivered != 0 {
			t.Fatalf("status %d: Send = %d, %v; want 0, nil", status, delivered, err)
		}
		n, _ := svc.Count(ctx, user.ID)
		if n != 0 {
			t.Fatalf("status %d: subscription survived, count = %d", status, n)
		}
	}
}

// Anything else — a 5xx, a rate limit, no answer — is the push service having
// a bad moment, not the browser leaving. The row stays and is marked.
func TestSendKeepsAndMarksASubscriptionThatMerelyFailed(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "push-failed@north.test")

	cases := map[string]*fakeSender{
		"server error":  {status: 500},
		"rate limited":  {status: 429},
		"network error": {err: errors.New("dial tcp: connection refused")},
	}
	for name, sender := range cases {
		t.Run(name, func(t *testing.T) {
			svc := newService(pool, sender)
			endpoint := "https://push.example/" + strings.ReplaceAll(name, " ", "-")
			if _, err := svc.Subscribe(ctx, user.ID, validInput(endpoint)); err != nil {
				t.Fatal(err)
			}

			delivered, err := svc.Send(ctx, user.ID, "t", "b", "/app")
			if err != nil {
				t.Fatalf("a refused send must not fail the caller: %v", err)
			}
			if delivered != 0 {
				t.Fatalf("delivered = %d, want 0", delivered)
			}

			subs, err := push.NewRepository(pool).ListByUser(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, s := range subs {
				if s.Endpoint != endpoint {
					continue
				}
				found = true
				if s.FailedAt == nil {
					t.Error("failed send did not stamp failed_at")
				}
				if s.LastUsedAt != nil {
					t.Error("failed send stamped last_used_at")
				}
			}
			if !found {
				t.Fatal("a merely failed subscription was deleted")
			}
		})
	}
}

func TestUnsubscribeIsScopedToTheOwner(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "push-owner@north.test")
	other := seedUser(t, pool, "push-other@north.test")
	svc := newService(pool, &fakeSender{status: 201})

	if _, err := svc.Subscribe(ctx, owner.ID, validInput("https://push.example/mine")); err != nil {
		t.Fatal(err)
	}

	if err := svc.Unsubscribe(ctx, other.ID, "https://push.example/mine"); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.Count(ctx, owner.ID); n != 1 {
		t.Fatalf("somebody else unsubscribed the owner's browser; count = %d", n)
	}

	if err := svc.Unsubscribe(ctx, owner.ID, "https://push.example/mine"); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.Count(ctx, owner.ID); n != 0 {
		t.Fatalf("owner could not unsubscribe; count = %d", n)
	}

	// Forgetting what is already gone is success.
	if err := svc.Unsubscribe(ctx, owner.ID, "https://push.example/mine"); err != nil {
		t.Fatalf("second unsubscribe: %v", err)
	}
}
