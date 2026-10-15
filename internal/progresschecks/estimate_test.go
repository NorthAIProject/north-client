package progresschecks_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/progresschecks"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// stubClient answers Generate with canned replies and records what it was
// sent. No real key is involved anywhere in these tests.
type stubClient struct {
	name    string
	mu      sync.Mutex
	replies []string
	err     error
	reqs    []ai.Request
}

func (c *stubClient) Name() string { return c.name }

func (c *stubClient) Chat(context.Context, ai.Request) (<-chan ai.StreamChunk, error) {
	return nil, errors.New("stub: chat unused")
}

func (c *stubClient) UploadFile(context.Context, ai.UploadRequest) (*ai.File, error) {
	return nil, errors.New("stub: upload unused")
}

func (c *stubClient) Generate(_ context.Context, req ai.Request) (*ai.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reqs = append(c.reqs, req)
	if c.err != nil {
		return nil, c.err
	}
	reply := c.replies[0]
	if len(c.replies) > 1 {
		c.replies = c.replies[1:]
	}
	return &ai.Response{Text: reply}, nil
}

func (c *stubClient) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reqs)
}

// stubOwn is a person's connected provider.
type stubOwn struct {
	provider string // empty: nothing connected
	client   ai.Client
}

func (o stubOwn) Get(context.Context, uuid.UUID) (aicreds.Credential, error) {
	if o.provider == "" {
		return aicreds.Credential{}, apperr.ErrNotFound
	}
	return aicreds.Credential{Provider: o.provider}, nil
}

func (o stubOwn) For(context.Context, uuid.UUID) (ai.Client, error) {
	if o.provider == "" {
		return nil, nil
	}
	return o.client, nil
}

const clearReply = `{"notes":"Clear photos.","confidence":"medium","waist_cm":95.2,"hips_cm":104,"chest_cm":0}`

func TestEstimateReadsTheReplyAndCleansIt(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{clearReply}}
	f.svc.WithAssist(progresschecks.NewAssist(stubOwn{}, app))

	est, dest, err := f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Front), jpeg(progresscheck.Side)})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if dest.Own || dest.Label != "Anthropic (Claude)" {
		t.Errorf("destination = %+v, want Khepri's Anthropic client", dest)
	}
	if est.WaistCm == nil || *est.WaistCm != 95.2 || est.HipsCm == nil || *est.HipsCm != 104 {
		t.Errorf("estimate = %+v", est)
	}
	if est.ChestCm != nil {
		t.Errorf("chest 0 means not visible, got %v", *est.ChestCm)
	}

	req := app.reqs[0]
	if req.Temperature != nil {
		t.Error("Haiku 5.5 refuses a non-default temperature; none may be sent")
	}
	if req.ResponseSchema == nil || !strings.Contains(req.System, "Never comment on the person's appearance") {
		t.Error("the estimate must be schema-constrained and carry the prompt's limits")
	}
	if images := countImages(req); images != 2 {
		t.Errorf("sent %d images, want today's two", images)
	}

	// Nothing is stored by an estimate.
	if len(f.store.objs) != 0 {
		t.Errorf("an estimate stored %d objects", len(f.store.objs))
	}
}

func TestEstimateSendsTheLastMeasuredCheckAsScale(t *testing.T) {
	f := newFixture(t)
	f.recordBaseline(t)
	if _, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		Measurements: progresscheck.Measurements{WaistCm: ptr(98.5)},
	}, []progresschecks.PhotoUpload{jpeg(progresscheck.Front), jpeg(progresscheck.Side)}); err != nil {
		t.Fatalf("save: %v", err)
	}

	app := &stubClient{name: "anthropic", replies: []string{clearReply}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))
	if _, _, err := f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Front)}); err != nil {
		t.Fatalf("estimate: %v", err)
	}

	req := app.reqs[0]
	if images := countImages(req); images != 3 {
		t.Errorf("sent %d images, want today's one and the earlier two", images)
	}
	text := allText(req)
	for _, want := range []string{"waist at 98.5 cm", "Height: 185 cm.", "notes in English"} {
		if !strings.Contains(text, want) {
			t.Errorf("request text lacks %q:\n%s", want, text)
		}
	}
}

func TestEstimateUnusablePhotosComeBackEmptyAndLow(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{
		`{"notes":"Side photo is cropped at the waist.","confidence":"high","waist_cm":0,"hips_cm":0,"chest_cm":0}`,
	}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))

	est, _, err := f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Side)})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !est.Empty() || est.Confidence != "low" || est.Notes == "" {
		t.Errorf("want nothing estimated, low, with the reason; got %+v", est)
	}
}

func TestEstimateRetriesOnceOnBadJSON(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{"Sure! The waist looks about 95 cm.", clearReply}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))

	est, _, err := f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Front)})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if app.calls() != 2 || est.WaistCm == nil {
		t.Errorf("calls = %d, estimate = %+v; want one retry that succeeds", app.calls(), est)
	}

	bad := &stubClient{name: "anthropic", replies: []string{"not json"}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, bad))
	_, _, err = f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Front)})
	if !apperr.Is(err, apperr.ErrUnavailable) || bad.calls() != 2 {
		t.Errorf("err = %v after %d calls; want unavailable after exactly two", err, bad.calls())
	}
}

func TestEstimateGoesToTheConnectedProviderWhenItSees(t *testing.T) {
	f := newFixture(t)
	own := &stubClient{name: "openai", replies: []string{clearReply}}
	app := &stubClient{name: "anthropic", replies: []string{clearReply}}
	f.svc.WithAssist(progresschecks.NewAssist(stubOwn{provider: "openai", client: own}, app))

	_, dest, err := f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Front)})
	if err != nil {
		t.Fatalf("estimate: %v", err)
	}
	if !dest.Own || dest.Label != "OpenAI" || own.calls() != 1 || app.calls() != 0 {
		t.Errorf("dest = %+v, own calls %d, app calls %d; want the person's OpenAI only", dest, own.calls(), app.calls())
	}
}

func TestEstimateNeverFailsOverToAnotherProvider(t *testing.T) {
	f := newFixture(t)
	own := &stubClient{name: "openai", err: errors.New("401 bad key")}
	app := &stubClient{name: "anthropic", replies: []string{clearReply}}
	f.svc.WithAssist(progresschecks.NewAssist(stubOwn{provider: "openai", client: own}, app))

	_, _, err := f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Front)})
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
	if app.calls() != 0 {
		t.Error("photos named for OpenAI went to Anthropic after a failure")
	}
}

func TestEstimatePassesOverAProviderThatCannotSee(t *testing.T) {
	f := newFixture(t)
	textOnly := &stubClient{name: "nvidia"}
	app := &stubClient{name: "anthropic", replies: []string{clearReply}}
	f.svc.WithAssist(progresschecks.NewAssist(stubOwn{provider: "nvidia", client: textOnly}, app))

	dest, err := f.svc.AssistDestination(f.ctx, f.user)
	if err != nil || dest.Own {
		t.Fatalf("dest = %+v, err %v; want Khepri's client over a text-only key", dest, err)
	}
	if textOnly.calls() != 0 {
		t.Error("a text-only provider was sent photos")
	}
}

func TestEstimateWithNoProviderIsUnavailable(t *testing.T) {
	f := newFixture(t)

	// No assist configured at all: Khepri has no Anthropic key and the
	// person connected nothing. There is no chain to fall back on.
	if _, err := f.svc.AssistDestination(f.ctx, f.user); !errors.Is(err, progresschecks.ErrAssistUnavailable) {
		t.Errorf("unconfigured: err = %v", err)
	}
	f.svc.WithAssist(progresschecks.NewAssist(stubOwn{provider: "hermes", client: &stubClient{name: "hermes"}}, nil))
	_, _, err := f.svc.Estimate(f.ctx, f.user, []progresschecks.PhotoUpload{jpeg(progresscheck.Front)})
	if !errors.Is(err, progresschecks.ErrAssistUnavailable) {
		t.Errorf("text-only key and no app client: err = %v", err)
	}
}

func TestEstimateRefusesPhotosItCannotSend(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{clearReply}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))

	pdf := progresschecks.PhotoUpload{Pose: progresscheck.Front, Body: bytes.NewReader([]byte("%PDF-1.7 not a photo"))}
	cases := map[string][]progresschecks.PhotoUpload{
		"not an image": {pdf},
		"back only":    {jpeg(progresscheck.Back)},
		"no photos":    nil,
		"two fronts":   {jpeg(progresscheck.Front), jpeg(progresscheck.Front)},
	}
	for name, uploads := range cases {
		_, _, err := f.svc.Estimate(f.ctx, f.user, uploads)
		var fe apperr.FieldErrors
		if !apperr.As(err, &fe) {
			t.Errorf("%s: err = %v, want a field error", name, err)
		}
	}
	if app.calls() != 0 {
		t.Errorf("refused photos still reached the provider (%d calls)", app.calls())
	}
}

func countImages(req ai.Request) int {
	n := 0
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if len(p.InlineData) > 0 {
				n++
			}
		}
	}
	return n
}

func allText(req ai.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			b.WriteString(p.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}
