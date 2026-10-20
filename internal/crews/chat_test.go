package crews_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/moderation"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
)

// room is a crew with an owner and two members who have all accepted the
// terms, and the chat service over it.
type room struct {
	pool    *pgxpool.Pool
	mod     *moderation.Service
	svc     *crews.Service
	chat    *crews.Chat
	crew    crews.Crew
	general uuid.UUID
	owner   users.User
	ana     users.User
	leo     users.User
}

func newRoom(t *testing.T) room {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	r := room{pool: pool, svc: service(pool)}
	r.mod = moderation.NewService(pool)
	r.chat = crews.NewChat(pool, r.mod, moderation.NewFilter(), jobs.NewQueue(pool))
	r.mod.Register(crews.MessageSubject, r.chat.Hider())
	r.owner = handled(t, pool, "owner@north.test", "Owner", "owner")
	r.ana = handled(t, pool, "ana@north.test", "Ana", "ana")
	r.leo = handled(t, pool, "leo@north.test", "Leo", "leo")
	var err error
	if r.crew, err = r.svc.Create(ctx, r.owner.ID, "Iron rats"); err != nil {
		t.Fatal(err)
	}
	for _, u := range []users.User{r.ana, r.leo} {
		if _, err = r.svc.Join(ctx, u.ID, r.crew.Code); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []users.User{r.owner, r.ana, r.leo} {
		if err = moderation.NewService(pool).AcceptTerms(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	channels, err := r.svc.Channels(ctx, r.crew.ID, r.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.general = channels[0].ID
	return r
}

// handled is a person with a handle, so others can @mention them.
func handled(t *testing.T, pool *pgxpool.Pool, email, name, handle string) users.User {
	t.Helper()
	u := person(t, pool, email, name)
	if err := social.NewRepository(pool).SetHandle(context.Background(), u.ID, &handle); err != nil {
		t.Fatal(err)
	}
	return u
}

func (r room) send(t *testing.T, author users.User, body string) crews.Message {
	t.Helper()
	m, err := r.chat.Send(context.Background(), r.crew.ID, r.general, author.ID, crews.SendInput{Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// firstListedTerm is the first term on the English filter list, read from
// the file so the test does not spell one out.
func firstListedTerm(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../moderation/wordlists/en.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	t.Fatal("empty word list")
	return ""
}

func bodies(ms []crews.Message) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if m.Kind == crews.MessageKindText {
			out = append(out, m.Body)
		}
	}
	return out
}

func TestSendAndList(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	m1 := r.send(t, r.ana, "one")
	m2 := r.send(t, r.ana, "two")
	m3 := r.send(t, r.ana, "three")
	if !(m1.Seq < m2.Seq && m2.Seq < m3.Seq) {
		t.Fatalf("seqs %d %d %d do not increase", m1.Seq, m2.Seq, m3.Seq)
	}

	page, err := r.chat.List(ctx, r.crew.ID, r.general, r.leo.ID, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(page.Messages); !slices.Equal(got, []string{"one", "two", "three"}) {
		t.Fatalf("newest page = %v", got)
	}
	if page.Messages[len(page.Messages)-1].Author == nil || page.Messages[len(page.Messages)-1].Author.Name != "Ana" {
		t.Fatalf("author = %+v", page.Messages[len(page.Messages)-1].Author)
	}

	after, err := r.chat.List(ctx, r.crew.ID, r.general, r.leo.ID, 0, m1.Seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(after.Messages); !slices.Equal(got, []string{"two", "three"}) {
		t.Fatalf("after %d = %v", m1.Seq, got)
	}

	before, err := r.chat.List(ctx, r.crew.ID, r.general, r.leo.ID, m3.Seq, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(before.Messages); !slices.Equal(got, []string{"two"}) || !before.HasMore {
		t.Fatalf("before %d limit 1 = %v, more %v", m3.Seq, got, before.HasMore)
	}
}

func TestSendValidates(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	send := func(author uuid.UUID, body string) error {
		_, err := r.chat.Send(ctx, r.crew.ID, r.general, author, crews.SendInput{Body: body})
		return err
	}

	if err := send(r.ana.ID, "   "); fieldError(err, "body") == "" {
		t.Errorf("empty body: %v", err)
	}
	if err := send(r.ana.ID, strings.Repeat("é", 4001)); fieldError(err, "body") == "" {
		t.Errorf("4001 runes: %v", err)
	}
	slur := firstListedTerm(t)
	if err := send(r.ana.ID, "you "+slur); fieldError(err, "body") == "" {
		t.Errorf("filtered term: %v", err)
	}
	stranger := handled(t, r.pool, "zed@north.test", "Zed", "zed")
	if err := send(stranger.ID, "hi"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("non-member: %v", err)
	}
	newcomer := handled(t, r.pool, "mia@north.test", "Mia", "mia")
	if _, err := r.svc.Join(ctx, newcomer.ID, r.crew.Code); err != nil {
		t.Fatal(err)
	}
	if err := send(newcomer.ID, "hi"); !errors.Is(err, moderation.ErrTermsRequired) {
		t.Errorf("terms not accepted: %v", err)
	}

	for i := range 20 {
		if err := send(r.leo.ID, "rep"); err != nil {
			t.Fatalf("send %d: %v", i+1, err)
		}
	}
	if err := send(r.leo.ID, "one too many"); fieldError(err, "body") == "" {
		t.Errorf("21st send in a minute: %v", err)
	}
}

func TestSendIdempotentClientID(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	id := uuid.New()
	a, err := r.chat.Send(ctx, r.crew.ID, r.general, r.ana.ID, crews.SendInput{Body: "once", ClientID: &id})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.chat.Send(ctx, r.crew.ID, r.general, r.ana.ID, crews.SendInput{Body: "once", ClientID: &id})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("two sends with one client id made %v and %v", a.ID, b.ID)
	}
	var n int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM crew_messages WHERE client_id = $1`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d rows, %v; want 1", n, err)
	}
}

func TestMentionsResolveToMembers(t *testing.T) {
	r := newRoom(t)
	handled(t, r.pool, "zed@north.test", "Zed", "zed")
	m := r.send(t, r.ana, "@leo spot me, @zed too")
	if !slices.Equal(m.Mentions, []uuid.UUID{r.leo.ID}) {
		t.Fatalf("mentions = %v; want only Leo", m.Mentions)
	}

	var all bool
	member := r.send(t, r.ana, "@everyone legs")
	if err := r.pool.QueryRow(context.Background(), `SELECT mention_all FROM crew_messages WHERE id = $1`, member.ID).Scan(&all); err != nil || all {
		t.Fatalf("@everyone from a member: mention_all %v, %v", all, err)
	}
	owner := r.send(t, r.owner, "@everyone legs")
	if err := r.pool.QueryRow(context.Background(), `SELECT mention_all FROM crew_messages WHERE id = $1`, owner.ID).Scan(&all); err != nil || !all {
		t.Fatalf("@everyone from the owner: mention_all %v, %v", all, err)
	}
}

// Ana blocked Leo: his messages are gone from her view, not from anyone
// else's, and her reply to one of them quotes nothing.
func TestBlockedAuthorsHidden(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	leos := r.send(t, r.leo, "from leo")
	reply, err := r.chat.Send(ctx, r.crew.ID, r.general, r.ana.ID, crews.SendInput{Body: "replying", ReplyToID: &leos.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = social.NewRepository(r.pool).Block(ctx, r.ana.ID, r.leo.ID); err != nil {
		t.Fatal(err)
	}

	anas, err := r.chat.List(ctx, r.crew.ID, r.general, r.ana.ID, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(anas.Messages); !slices.Equal(got, []string{"replying"}) {
		t.Fatalf("Ana sees %v", got)
	}
	if q := anas.Messages[len(anas.Messages)-1].ReplyTo; q == nil || !q.Unavailable || q.Excerpt != "" {
		t.Fatalf("Ana's quote = %+v; want unavailable", q)
	}

	owners, err := r.chat.List(ctx, r.crew.ID, r.general, r.owner.ID, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(owners.Messages); !slices.Equal(got, []string{"from leo", "replying"}) {
		t.Fatalf("Owner sees %v", got)
	}
	got, err := r.chat.Get(ctx, r.crew.ID, reply.ID, r.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReplyTo == nil || got.ReplyTo.Unavailable || got.ReplyTo.Excerpt != "from leo" || got.ReplyTo.AuthorName != "Leo" {
		t.Fatalf("Owner's quote = %+v", got.ReplyTo)
	}
	if _, err = r.chat.Get(ctx, r.crew.ID, leos.ID, r.ana.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("Ana getting Leo's message: %v", err)
	}
}

func TestSendNotifiesAndQueues(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, r.pool.Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err = conn.Exec(ctx, "LISTEN "+live.Channel); err != nil {
		t.Fatal(err)
	}

	m := r.send(t, r.ana, "ping")

	wait, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n, err := conn.WaitForNotification(wait)
	if err != nil {
		t.Fatal(err)
	}
	var ev live.Event
	if err = json.Unmarshal([]byte(n.Payload), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Type != live.EventMessage || ev.ChannelID != r.general || ev.Seq != m.Seq || ev.MessageID != m.ID || ev.CrewID != r.crew.ID {
		t.Fatalf("event = %+v", ev)
	}

	var jobsQueued int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind = $1 AND payload->>'message_id' = $2`,
		string(jobs.KindCrewMessagePush), m.ID.String()).Scan(&jobsQueued); err != nil || jobsQueued != 1 {
		t.Fatalf("%d push jobs, %v; want 1", jobsQueued, err)
	}
}
