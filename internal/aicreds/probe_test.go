package aicreds_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// gatedProbe answers what it was told to, when it is let go. No provider is ever
// reached.
type gatedProbe struct {
	answer  bool
	entered chan struct{}
	release chan struct{}
}

func (p *gatedProbe) SupportsTools(context.Context, ai.Client, string) (bool, error) {
	if p.entered != nil {
		close(p.entered)
	}
	if p.release != nil {
		<-p.release
	}
	return p.answer, nil
}

const (
	modelA = "anthropic/claude-sonnet-4.5"
	modelB = "openai/gpt-4.1"
)

func supportsTools(t *testing.T, svc *aicreds.Service, userID uuid.UUID) *bool {
	t.Helper()
	cred, err := svc.Get(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return cred.SupportsTools
}

// The defect: a user saves one provider and then another, and the first one's
// slow probe lands last and stamps its answer on the second.
func TestASlowProbeDoesNotOverwriteANewerCredential(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: modelA})

	slow := &gatedProbe{answer: false, entered: make(chan struct{}), release: make(chan struct{})}
	svc.WithToolProbe(slow)

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.ProbeTools(ctx, user.ID)
	}()
	<-slow.entered

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: modelB})

	close(slow.release)
	<-done

	if got := supportsTools(t, svc, user.ID); got != nil {
		t.Fatalf("supports_tools = %v: an older credential's probe overwrote the newer one", *got)
	}

	// And the newer credential's own probe still records.
	svc.WithToolProbe(&gatedProbe{answer: true})
	svc.ProbeTools(ctx, user.ID)
	got := supportsTools(t, svc, user.ID)
	if got == nil || !*got {
		t.Fatalf("supports_tools = %v, want true from the current credential's probe", got)
	}
}

// keyHookRepo runs a hook just after the stored key is read: a save landing in the
// middle of ProbeTools, between building the client and recording the answer.
type keyHookRepo struct {
	aicreds.Repository
	once  sync.Once
	after func()
}

func (r *keyHookRepo) Key(ctx context.Context, userID uuid.UUID) (aicreds.Credential, aicreds.Sealed, error) {
	cred, sealed, err := r.Repository.Key(ctx, userID)
	r.once.Do(func() {
		if r.after != nil {
			r.after()
		}
	})
	return cred, sealed, err
}

// The narrow form of the same race. The client that was probed is A's, so A's
// answer must not be recorded as the answer for whatever is stored by then.
func TestASaveDuringTheProbesSetupDoesNotPassAsThatProbesCredential(t *testing.T) {
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}

	repo := &keyHookRepo{Repository: aicreds.NewRepository(pool)}
	svc := aicreds.NewService(repo, sealer(t, 1), nil).WithVerifier(&stubVerifier{})
	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: modelA})

	repo.once = sync.Once{}
	repo.after = func() {
		// Far enough from the first save that the versions cannot coincide.
		time.Sleep(2 * time.Millisecond)
		save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: modelB})
	}
	svc.WithToolProbe(&gatedProbe{answer: false})
	svc.ProbeTools(context.Background(), user.ID)

	if got := supportsTools(t, svc, user.ID); got != nil {
		t.Fatalf("supports_tools = %v: recorded against a credential that was not the one probed", *got)
	}
}

// A flag from the previous provider says nothing about this one. If the new
// probe cannot reach it, unknown is the truth and a stale answer is not, and a
// stale false takes the coach's write tools away.
func TestSavingForgetsWhatTheLastProbeFound(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		next aicreds.Input
	}{
		{"a new key", aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: modelA}},
		{"a blank key with a new model", aicreds.Input{Provider: "openrouter", Model: modelB}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, user := newService(t, sealer(t, 1))
			save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: modelA})

			svc.WithToolProbe(&gatedProbe{answer: false})
			svc.ProbeTools(ctx, user.ID)
			if got := supportsTools(t, svc, user.ID); got == nil || *got {
				t.Fatalf("supports_tools = %v after the probe, want false", got)
			}

			save(t, svc, user.ID, tc.next)
			if got := supportsTools(t, svc, user.ID); got != nil {
				t.Errorf("supports_tools = %v after saving, want unknown", *got)
			}
		})
	}
}

// Nothing racing: the answer lands, which is the whole job.
func TestAProbeWithNoRaceRecordsItsAnswer(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: modelA})

	svc.WithToolProbe(&gatedProbe{answer: true})
	svc.ProbeTools(context.Background(), user.ID)

	if got := supportsTools(t, svc, user.ID); got == nil || !*got {
		t.Fatalf("supports_tools = %v, want true", got)
	}
}
