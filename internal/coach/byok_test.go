package coach_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// stubSource stands in for aicreds.Service: one client, or an error, plus a
// record of what was reported back about it.
type stubSource struct {
	client ai.Client
	err    error

	mu       sync.Mutex
	asked    int
	failures []string
}

func (s *stubSource) For(context.Context, uuid.UUID) (ai.Client, error) {
	s.mu.Lock()
	s.asked++
	s.mu.Unlock()
	return s.client, s.err
}

func (s *stubSource) NoteFailure(_ context.Context, _ uuid.UUID, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, reason)
}

func (s *stubSource) noted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.failures...)
}

// byokHarness builds a coach whose chain is Khepri's providers, with a
// ClientSource in front.
func byokHarness(t *testing.T, own coach.ClientSource, chain ...ai.Client) harness {
	t.Helper()

	h := newHarness(t, fake.Text("unused"))

	registry := ai.NewRegistry()
	names := make([]string, 0, len(chain))
	for _, c := range chain {
		registry.Register(c)
		names = append(names, c.Name())
	}

	convos := conversations.NewService(conversations.NewRepository(h.pool))
	h.coach = coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains:         ai.NewChainSet(names, nil),
		Own:            own,
	})
	h.convos = convos

	return h
}

func ask(t *testing.T, h harness) string {
	t.Helper()

	ctx := context.Background()
	conversation, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "what next?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	reply, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	return reply
}

// The point of BYOK: somebody who supplied a key is served by it.
func TestOwnProviderIsTriedBeforeNorthsChain(t *testing.T) {
	north := renamed{Client: fake.Text("from Khepri"), name: "north"}
	mine := renamed{Client: fake.Text("from my own key"), name: "mine"}

	source := &stubSource{client: mine}
	h := byokHarness(t, source, north)

	if reply := ask(t, h); !strings.Contains(reply, "from my own key") {
		t.Fatalf("reply = %q, want the user's own provider", reply)
	}
	if len(north.Calls()) != 0 {
		t.Errorf("Khepri's provider was called %d time(s) despite a working user key", len(north.Calls()))
	}
	if noted := source.noted(); len(noted) != 0 {
		t.Errorf("a working key was reported as failing: %v", noted)
	}
}

// A mistyped key must not take the product away. It falls back — and says so,
// because a silent fallback leaves somebody believing their key is in use.
func TestARejectedKeyFallsBackAndIsReported(t *testing.T) {
	north := renamed{Client: fake.Text("from Khepri"), name: "north"}
	mine := refusing("mine", apperr.Wrap(apperr.ErrForbidden, "invalid api key"))

	source := &stubSource{client: mine}
	h := byokHarness(t, source, north)

	if reply := ask(t, h); !strings.Contains(reply, "from Khepri") {
		t.Fatalf("reply = %q, want Khepri's chain to have answered", reply)
	}

	// One turn makes more than one model call — the reply, and the cheap one
	// that names the thread — so a dead key is reported more than once. The
	// write overwrites the same column with the same string, so the count is
	// not worth constraining; the content is.
	noted := source.noted()
	if len(noted) == 0 {
		t.Fatal("a rejected key was not reported anywhere the user could see it")
	}
	if !strings.Contains(noted[0], "rejected the key") {
		t.Errorf("reason = %q, want it to name a rejected key", noted[0])
	}
	// The reason is written for the person who owns the key, and must not
	// carry the provider's own response, which can echo the credential.
	if strings.Contains(noted[0], "invalid api key") {
		t.Errorf("reason = %q, want a summary rather than the provider's error", noted[0])
	}
}

func TestAnExhaustedKeyIsReportedAsBilling(t *testing.T) {
	north := renamed{Client: fake.Text("from Khepri"), name: "north"}
	mine := refusing("mine", apperr.Wrap(apperr.ErrPaymentRequired, "out of credit"))

	source := &stubSource{client: mine}
	h := byokHarness(t, source, north)

	if reply := ask(t, h); !strings.Contains(reply, "from Khepri") {
		t.Fatalf("reply = %q, want Khepri's chain to have answered", reply)
	}
	if noted := source.noted(); len(noted) == 0 || !strings.Contains(noted[0], "credit") {
		t.Fatalf("reason = %v, want it to name the billing cause", noted)
	}
}

// A credential that cannot be decrypted or built is Khepri's problem, not a
// reason to refuse the user their coach.
func TestAnUnbuildableCredentialFallsBackSilently(t *testing.T) {
	north := renamed{Client: fake.Text("from Khepri"), name: "north"}

	source := &stubSource{err: apperr.New("sealed value cannot be opened")}
	h := byokHarness(t, source, north)

	if reply := ask(t, h); !strings.Contains(reply, "from Khepri") {
		t.Fatalf("reply = %q, want Khepri's chain to have answered", reply)
	}
	// Nothing to tell the user: they did not do anything wrong, and a message
	// about decryption would only alarm them.
	if noted := source.noted(); len(noted) != 0 {
		t.Errorf("failures recorded = %v, want none", noted)
	}
}

// A user with no key of their own is the normal case, and (nil, nil) is how
// the source says so. It must not read as an error.
func TestNoCredentialIsNotAnError(t *testing.T) {
	north := renamed{Client: fake.Text("from Khepri"), name: "north"}

	source := &stubSource{}
	h := byokHarness(t, source, north)

	if reply := ask(t, h); !strings.Contains(reply, "from Khepri") {
		t.Fatalf("reply = %q, want Khepri's chain to have answered", reply)
	}
	if source.asked == 0 {
		t.Error("the credential source was never consulted")
	}
}

// The regression guard for every existing caller: cmd/worker and the tests
// pass no source at all.
func TestANilSourceChangesNothing(t *testing.T) {
	north := renamed{Client: fake.Text("from Khepri"), name: "north"}

	h := byokHarness(t, nil, north)

	if reply := ask(t, h); !strings.Contains(reply, "from Khepri") {
		t.Fatalf("reply = %q, want Khepri's chain to have answered", reply)
	}
}

// A user key that refuses for a caller-side reason fails the same way
// everywhere, so walking on to Khepri's provider only delays the same error.
func TestACallerErrorFromTheUserKeyDoesNotWalkTheChain(t *testing.T) {
	north := renamed{Client: fake.Text("from Khepri"), name: "north"}
	mine := refusing("mine", apperr.Wrap(apperr.ErrValidation, "malformed request"))

	source := &stubSource{client: mine}
	h := byokHarness(t, source, north)

	ctx := context.Background()
	conversation, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "what next?")
	if err == nil {
		if _, drainErr := drain(stream); drainErr == nil {
			t.Fatal("a malformed request was retried against Khepri's provider")
		}
	}
	if len(north.Calls()) != 0 {
		t.Errorf("Khepri's provider was called %d time(s) for a caller-side error", len(north.Calls()))
	}
}
