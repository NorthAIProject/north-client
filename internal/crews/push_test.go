package crews_test

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
)

type sent struct {
	userID                                uuid.UUID
	title, body, href, category, threadID string
}

// pushRecorder is a ThreadSender that remembers what it was asked to send.
type pushRecorder struct {
	mu   sync.Mutex
	sent []sent
}

func (p *pushRecorder) SendThread(_ context.Context, userID uuid.UUID, title, body, href, category, threadID string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, sent{userID, title, body, href, category, threadID})
	return 1, nil
}

func (p *pushRecorder) take() []sent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.sent
	p.sent = nil
	return out
}

func pushJob(t *testing.T, chat *crews.Chat, author users.User, m crews.Message) {
	t.Helper()
	raw, err := json.Marshal(jobs.CrewMessagePushPayload{UserID: author.ID, MessageID: m.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = chat.HandlePushJob(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
}

func recipients(s []sent) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(s))
	for _, p := range s {
		out = append(out, p.userID)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out
}

func ids(us ...users.User) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(us))
	for _, u := range us {
		out = append(out, u.ID)
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out
}

func TestPushRecipients(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	rec := &pushRecorder{}
	chat := r.chat.WithPushers(rec)
	more := r.grow(t, 3)
	kai, mia, zed := more[0], more[1], more[2]

	if err := r.svc.SetNotify(ctx, r.crew.ID, r.owner.ID, "all", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetNotify(ctx, r.crew.ID, r.leo.ID, "none", nil); err != nil {
		t.Fatal(err)
	}
	muted := time.Now().Add(time.Hour)
	if err := r.svc.SetNotify(ctx, r.crew.ID, mia.ID, "all", &muted); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.SetNotify(ctx, r.crew.ID, zed.ID, "all", nil); err != nil {
		t.Fatal(err)
	}
	if err := social.NewRepository(r.pool).Block(ctx, kai.ID, zed.ID); err != nil {
		t.Fatal(err)
	}

	plain := r.send(t, kai, "leg day at 6")
	pushJob(t, chat, kai, plain)
	got := rec.take()
	if !slices.Equal(recipients(got), ids(r.owner)) {
		t.Fatalf("a plain message reached %v; want only the owner", recipients(got))
	}
	p := got[0]
	if p.title != "Iron rats · #general" || p.body != "M0: leg day at 6" ||
		p.href != "/app/crews/"+r.crew.ID.String()+"/c/"+r.general.String() ||
		p.category != "CREW_MESSAGE" || p.threadID != "crew-"+r.crew.ID.String() {
		t.Fatalf("push = %+v", p)
	}

	pushJob(t, chat, kai, r.send(t, kai, "@ana coming?"))
	if got := recipients(rec.take()); !slices.Equal(got, ids(r.owner, r.ana)) {
		t.Fatalf("@ana reached %v; want the owner and Ana", got)
	}

	leos := r.send(t, r.leo, "who's in?")
	reply, err := r.chat.Send(ctx, r.crew.ID, r.general, kai.ID, crews.SendInput{Body: "me", ReplyToID: &leos.ID})
	if err != nil {
		t.Fatal(err)
	}
	pushJob(t, chat, kai, reply)
	if got := recipients(rec.take()); !slices.Equal(got, ids(r.owner)) {
		t.Fatalf("a reply to Leo (notify none) reached %v; want only the owner", got)
	}

	anas := r.send(t, r.ana, "who's in?")
	reply, err = r.chat.Send(ctx, r.crew.ID, r.general, kai.ID, crews.SendInput{Body: "me too", ReplyToID: &anas.ID})
	if err != nil {
		t.Fatal(err)
	}
	pushJob(t, chat, kai, reply)
	if got := recipients(rec.take()); !slices.Equal(got, ids(r.owner, r.ana)) {
		t.Fatalf("a reply to Ana reached %v; want the owner and Ana", got)
	}
}

func TestPushSkipsDeletedOrHidden(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	rec := &pushRecorder{}
	chat := r.chat.WithPushers(rec)
	if err := r.svc.SetNotify(ctx, r.crew.ID, r.owner.ID, "all", nil); err != nil {
		t.Fatal(err)
	}

	gone := r.send(t, r.ana, "oops")
	if err := r.chat.Delete(ctx, r.crew.ID, gone.ID, r.ana.ID); err != nil {
		t.Fatal(err)
	}
	pushJob(t, chat, r.ana, gone)

	hidden := r.send(t, r.ana, "reported")
	if _, err := r.pool.Exec(ctx, `UPDATE crew_messages SET hidden_at = now() WHERE id = $1`, hidden.ID); err != nil {
		t.Fatal(err)
	}
	pushJob(t, chat, r.ana, hidden)

	pushJob(t, chat, r.ana, crews.Message{ID: uuid.New()})
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("pushed %+v; want nothing", got)
	}
}
