package dailyposts

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
)

// promptVersion is stored with every post. Bump it when daily_post.md or
// daily_post_review.md changes in a way worth telling apart later.
const promptVersion = "daily_post/1"

const (
	// topicAttempts is how many themes one run tries before giving the day up.
	// A theme the safety gate rejects twice in a row is usually the theme.
	topicAttempts = 2
	// draftAttempts is the JSON correction loop within one theme.
	draftAttempts = 2
	// earlierForTopic is how far back the rotation looks; earlierForPrompt is
	// how many titles the model is shown.
	earlierForTopic  = repeatWindow
	earlierForPrompt = 7
	postMaxTokens    = 700
	reviewMaxTokens  = 200
)

// Generator is the non-streaming AI call a post needs. Production passes
// reports.ClientFromChain, which walks the provider chain.
type Generator interface {
	Generate(ctx context.Context, req ai.Request) (*ai.Response, error)
}

// Writer publishes the day's post. Every run checks first, so the hourly job
// is a no-op once the day has one, and a failed hour is retried by the next.
type Writer struct {
	repo   *Repository
	client Generator
	model  string
	now    func() time.Time
	log    *slog.Logger
}

func NewWriter(repo *Repository, client Generator, model string, log *slog.Logger) *Writer {
	if log == nil {
		log = slog.Default()
	}
	return &Writer{repo: repo, client: client, model: model, now: time.Now, log: log}
}

// WithClock replaces the clock, for tests.
func (w *Writer) WithClock(now func() time.Time) *Writer {
	w.now = now
	return w
}

// HandleSweep is the hourly job.
func (w *Writer) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	_, err := w.Ensure(ctx)
	return err
}

// Ensure writes today's post unless it exists. published is true only when
// this call wrote it. A day the safety gate refuses every draft is not an
// error: there is simply no post, and the next hour tries again.
func (w *Writer) Ensure(ctx context.Context) (bool, error) {
	if w.client == nil {
		return false, nil
	}
	today := w.now().UTC()
	exists, err := w.repo.Exists(ctx, today)
	if err != nil || exists {
		return false, err
	}

	earlier, err := w.repo.EarlierThemes(ctx, today, earlierForTopic)
	if err != nil {
		return false, err
	}
	var signal *Signal
	week, err := w.repo.Week(ctx, today.AddDate(0, 0, -7), today)
	if err != nil {
		// The signal is a seasoning. Without it the rotation still picks.
		w.log.Warn("dailypost aggregate failed", slog.Any("error", err))
	} else if s, ok := SignalFrom(week); ok {
		signal = &s
	}

	ctx = aiattr.With(ctx, aiattr.Attribution{UserID: uuid.Nil, Surface: spend.SurfaceDailyPost})

	var tried []string
	for range topicAttempts {
		topic := PickTopic(today, earlier, signal, tried)
		tried = append(tried, topic.Theme.Key)

		d, err := w.draft(ctx, topic, earlier)
		if err != nil {
			return false, err
		}
		if problems := Screen(d.Title, d.Body); len(problems) > 0 {
			w.log.Warn("dailypost rejected", slog.String("theme", topic.Theme.Key),
				slog.String("by", "screen"), slog.String("reason", strings.Join(problems, "; ")))
			continue
		}
		ok, reason, err := w.review(ctx, d)
		if err != nil {
			return false, err
		}
		if !ok {
			w.log.Warn("dailypost rejected", slog.String("theme", topic.Theme.Key),
				slog.String("by", "review"), slog.String("reason", reason))
			continue
		}

		d.Date, d.Theme, d.PromptVersion = today, topic.Theme.Key, promptVersion
		inserted, err := w.repo.Insert(ctx, d)
		if err != nil {
			return false, err
		}
		if inserted {
			w.log.Info("dailypost published", slog.String("theme", topic.Theme.Key), slog.String("model", d.Model))
		}
		return inserted, nil
	}
	w.log.Warn("dailypost skipped", slog.String("date", today.Format(time.DateOnly)), slog.Any("themes", tried))
	return false, nil
}

type generatedPost struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func postSchema() *ai.Schema {
	return ai.Object("the daily post", map[string]*ai.Schema{
		"title": ai.String("the title, under 70 characters"),
		"body":  ai.String("the body, 120 to 200 words of plain text in short paragraphs"),
	}, "title", "body")
}

func (w *Writer) draft(ctx context.Context, topic Topic, earlier []Earlier) (Draft, error) {
	titles := make([]string, 0, earlierForPrompt)
	for i, e := range earlier {
		if i >= earlierForPrompt {
			break
		}
		titles = append(titles, e.Title)
	}
	prompt, err := prompts.Render(prompts.DailyPost, map[string]any{
		"Moment":  topic.Moment,
		"Theme":   topic.Theme.Brief,
		"Signal":  topic.Signal,
		"Earlier": titles,
	})
	if err != nil {
		return Draft{}, apperr.Wrap(err, "render the daily post prompt")
	}

	// No Temperature: the first provider in the chain refuses one.
	messages := []ai.Message{ai.UserText(prompt)}
	for attempt := 1; attempt <= draftAttempts; attempt++ {
		resp, err := w.client.Generate(ctx, ai.Request{
			Model: w.model, Messages: messages, ResponseSchema: postSchema(), MaxTokens: postMaxTokens,
		})
		if err != nil {
			return Draft{}, apperr.Wrap(err, "generate daily post")
		}
		var reply generatedPost
		if err := json.Unmarshal(ai.JSONReply(resp.Text), &reply); err == nil && reply.Body != "" {
			return Draft{
				Title: strings.TrimSpace(reply.Title), Body: strings.TrimSpace(reply.Body), Model: resp.Model,
			}, nil
		}
		w.log.Warn("dailypost did not decode", slog.Int("attempt", attempt))
		messages = append(messages,
			ai.ModelText(resp.Text),
			ai.UserText("That was not valid JSON matching the schema. Return the post again, correctly."),
		)
	}
	return Draft{}, apperr.Wrap(apperr.ErrUnavailable, "the model did not return a daily post")
}

type reviewVerdict struct {
	Approve bool   `json:"approve"`
	Reason  string `json:"reason"`
}

func reviewSchema() *ai.Schema {
	return ai.Object("the verdict", map[string]*ai.Schema{
		"approve": ai.Boolean("true to publish, false to reject"),
		"reason":  ai.String("one sentence"),
	}, "approve", "reason")
}

// review is the second opinion. A reply that does not decode is a rejection:
// when in doubt, no post.
func (w *Writer) review(ctx context.Context, d Draft) (bool, string, error) {
	prompt, err := prompts.Render(prompts.DailyPostReview, map[string]string{"Title": d.Title, "Body": d.Body})
	if err != nil {
		return false, "", apperr.Wrap(err, "render the daily post review prompt")
	}
	resp, err := w.client.Generate(ctx, ai.Request{
		Model: w.model, Messages: []ai.Message{ai.UserText(prompt)}, ResponseSchema: reviewSchema(), MaxTokens: reviewMaxTokens,
	})
	if err != nil {
		return false, "", apperr.Wrap(err, "review daily post")
	}
	var v reviewVerdict
	if err := json.Unmarshal(ai.JSONReply(resp.Text), &v); err != nil {
		return false, "the review did not decode", nil
	}
	return v.Approve, v.Reason, nil
}
