package dailyposts_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/dailyposts"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// 07:00 UTC on Tuesday 20 October 2026.
var tuesday = time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)

const body = `Most people drink less than they think, and the gap shows up as an afternoon
slump more often than as thirst. Water is easy to forget when the day gets busy,
which is exactly when a small habit helps.

Today, try one simple anchor: a glass of water before each meal. It ties drinking
to something you already do three times a day, so it needs no reminder and no
app. If plain water feels dull, a slice of lemon changes nothing that matters.

Notice how the afternoon feels by the end of the week. Steadier energy and fewer
headaches are the common reports, and the habit costs nothing to keep. If you
train, have another glass around your session, and more on hot days when you
sweat more than usual. Keep a glass or bottle where you can see it.`

func postJSON(title, body string) string {
	raw, _ := json.Marshal(map[string]string{"title": title, "body": body})
	return string(raw)
}

func verdict(approve bool) string {
	raw, _ := json.Marshal(map[string]any{"approve": approve, "reason": "because"})
	return string(raw)
}

func isReview(req ai.Request) bool {
	return strings.HasPrefix(req.Messages[0].Text(), "You review a short post")
}

// scripted answers drafts and reviews from two queues, so a test reads as
// the sequence of drafts and verdicts it expects.
type scripted struct {
	mu       sync.Mutex
	drafts   []string
	verdicts []string
}

func (s *scripted) client() *fake.Client {
	return &fake.Client{Handler: func(_ context.Context, req ai.Request) (fake.Response, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		q := &s.drafts
		if isReview(req) {
			q = &s.verdicts
		}
		if len(*q) == 0 {
			return fake.Response{Text: "{}"}, nil
		}
		next := (*q)[0]
		if len(*q) > 1 {
			*q = (*q)[1:]
		}
		return fake.Response{Text: next}, nil
	}}
}

type fixture struct {
	repo dailyposts.Repository
	svc  *dailyposts.Service
	user users.User
	ctx  context.Context
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: "daily@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName: "Reader", Timezone: "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	repo := dailyposts.NewRepository(pool)
	return fixture{
		repo: repo, user: user, ctx: context.Background(),
		svc: dailyposts.NewService(repo).WithClock(func() time.Time { return tuesday }),
	}
}

func (f fixture) writer(c ai.Client) *dailyposts.Writer {
	return dailyposts.NewWriter(f.repo, c, "fast", nil).WithClock(func() time.Time { return tuesday })
}

func TestEnsurePublishesOncePerDay(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	s := &scripted{drafts: []string{postJSON("Water before meals", body)}, verdicts: []string{verdict(true)}}
	c := s.client()

	published, err := f.writer(c).Ensure(f.ctx)
	if err != nil || !published {
		t.Fatalf("first run: published=%v err=%v", published, err)
	}
	calls := len(c.Calls())

	published, err = f.writer(c).Ensure(f.ctx)
	if err != nil || published {
		t.Fatalf("second run: published=%v err=%v", published, err)
	}
	if len(c.Calls()) != calls {
		t.Fatal("the second run called the model although today already has a post")
	}

	feed, err := f.svc.Feed(f.ctx, f.user.ID)
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	if feed.Today == nil || feed.Today.Title != "Water before meals" || !feed.Today.AIGenerated {
		t.Fatalf("today = %+v", feed.Today)
	}
}

func TestEnsureRacingWritersPublishOnePost(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	var wg sync.WaitGroup
	results := make(chan bool, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := &scripted{drafts: []string{postJSON("Water before meals", body)}, verdicts: []string{verdict(true)}}
			published, err := f.writer(s.client()).Ensure(f.ctx)
			if err != nil {
				t.Errorf("ensure: %v", err)
			}
			results <- published
		}()
	}
	wg.Wait()
	close(results)
	n := 0
	for p := range results {
		if p {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d writers published, want exactly 1", n)
	}
}

func TestEnsureTriesAnotherThemeWhenTheScreenRefuses(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	s := &scripted{
		drafts:   []string{postJSON("Magnesium at night", body+"\n\nTake 400 mg of magnesium."), postJSON("Water before meals", body)},
		verdicts: []string{verdict(true)},
	}
	c := s.client()

	published, err := f.writer(c).Ensure(f.ctx)
	if err != nil || !published {
		t.Fatalf("published=%v err=%v", published, err)
	}
	feed, _ := f.svc.Feed(f.ctx, f.user.ID)
	if feed.Today == nil || feed.Today.Title != "Water before meals" {
		t.Fatalf("today = %+v, want the second draft", feed.Today)
	}
	var reviews int
	for _, req := range c.Calls() {
		if isReview(req) {
			reviews++
		}
		if req.Temperature != nil {
			t.Fatal("a request set Temperature; the first provider refuses it")
		}
	}
	if reviews != 1 {
		t.Fatalf("%d reviews, want 1: the refused draft never reaches the reviewer", reviews)
	}
}

func TestEnsureSkipsTheDayWhenTheReviewerRefusesEveryDraft(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	s := &scripted{drafts: []string{postJSON("Water before meals", body)}, verdicts: []string{verdict(false)}}

	published, err := f.writer(s.client()).Ensure(f.ctx)
	if err != nil || published {
		t.Fatalf("published=%v err=%v, want a skipped day", published, err)
	}
	feed, err := f.svc.Feed(f.ctx, f.user.ID)
	if err != nil || feed.Today != nil {
		t.Fatalf("today = %+v err=%v, want none", feed.Today, err)
	}
}

func TestEnsureCorrectsAReplyThatIsNotJSON(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	s := &scripted{drafts: []string{"Here is your post!", postJSON("Water before meals", body)}, verdicts: []string{verdict(true)}}

	published, err := f.writer(s.client()).Ensure(f.ctx)
	if err != nil || !published {
		t.Fatalf("published=%v err=%v", published, err)
	}
}

func TestEnsureWithoutAClientDoesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	published, err := dailyposts.NewWriter(f.repo, nil, "", nil).Ensure(f.ctx)
	if err != nil || published {
		t.Fatalf("published=%v err=%v", published, err)
	}
}

func TestFeedSplitsTodayFromEarlierAndCountsKudos(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for d, title := range []string{"Two days ago", "Yesterday"} {
		if _, err := f.repo.Insert(f.ctx, dailyposts.Draft{
			Date: tuesday.AddDate(0, 0, d-2), Theme: "sleep", Title: title, Body: body,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	feed, err := f.svc.Feed(f.ctx, f.user.ID)
	if err != nil {
		t.Fatalf("feed: %v", err)
	}
	if feed.Today != nil {
		t.Fatalf("today = %+v, want none", feed.Today)
	}
	if len(feed.Earlier) != 2 || feed.Earlier[0].Title != "Yesterday" {
		t.Fatalf("earlier = %+v", feed.Earlier)
	}

	id := feed.Earlier[0].ID
	for range 2 {
		if err := f.svc.GiveKudos(f.ctx, id, f.user.ID); err != nil {
			t.Fatalf("kudos: %v", err)
		}
	}
	feed, _ = f.svc.Feed(f.ctx, f.user.ID)
	if feed.Earlier[0].Kudos != 1 || !feed.Earlier[0].Kudoed {
		t.Fatalf("after kudos twice: %+v", feed.Earlier[0])
	}
	if err := f.svc.TakeKudos(f.ctx, id, f.user.ID); err != nil {
		t.Fatalf("take: %v", err)
	}
	feed, _ = f.svc.Feed(f.ctx, f.user.ID)
	if feed.Earlier[0].Kudos != 0 || feed.Earlier[0].Kudoed {
		t.Fatalf("after taking it back: %+v", feed.Earlier[0])
	}

	if err := f.svc.GiveKudos(f.ctx, uuid.New(), f.user.ID); err == nil {
		t.Fatal("kudos on a post that does not exist succeeded")
	}
}
