package progresschecks_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/progresschecks"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const compareReply = `{"limits":"Different light.","confidence":"medium","summary":"The waist looks narrower from the side."}`

func (f *fixture) saveOn(t *testing.T, on time.Time, waist float64, poses ...progresscheck.Pose) progresschecks.Check {
	t.Helper()
	var uploads []progresschecks.PhotoUpload
	for _, p := range poses {
		uploads = append(uploads, jpeg(p))
	}
	c, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		MeasuredOn: on, Measurements: progresscheck.Measurements{WaistCm: ptr(waist)},
	}, uploads)
	if err != nil {
		t.Fatalf("save %s: %v", on.Format(time.DateOnly), err)
	}
	return c
}

func TestCompareNeedsTwoChecksWithPhotos(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{compareReply}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))

	_, err := f.svc.Compare(f.ctx, f.user)
	var fe apperr.FieldErrors
	if !apperr.As(err, &fe) {
		t.Fatalf("no checks: err = %v, want a field error", err)
	}

	f.saveOn(t, date(2026, 9, 1), 100, progresscheck.Front, progresscheck.Side)
	f.saveOn(t, date(2026, 10, 1), 99) // numbers only: nothing to compare
	if _, err := f.svc.Compare(f.ctx, f.user); !apperr.As(err, &fe) {
		t.Errorf("one check with photos: err = %v, want a field error", err)
	}
	if ok, _ := f.svc.CanCompare(f.ctx, f.user); ok {
		t.Error("CanCompare with one check with photos")
	}
	if app.calls() != 0 {
		t.Errorf("nothing to compare still called the provider %d times", app.calls())
	}
}

func TestCompareSendsFirstAndLatestAndStoresTheObservation(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{compareReply}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))

	first := f.saveOn(t, date(2026, 9, 1), 100, progresscheck.Front, progresscheck.Side, progresscheck.Back)
	f.saveOn(t, date(2026, 9, 15), 99, progresscheck.Front) // a middle check is not sent
	latest := f.saveOn(t, date(2026, 10, 15), 96.5, progresscheck.Side, progresscheck.Front)

	cmp, err := f.svc.Compare(f.ctx, f.user)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if cmp.FromCheckID != first.ID || cmp.ToCheckID != latest.ID {
		t.Errorf("compared %s → %s, want first → latest", cmp.FromCheckID, cmp.ToCheckID)
	}
	if cmp.Summary != "The waist looks narrower from the side." || cmp.Confidence != "medium" || cmp.Provider != "Anthropic (Claude)" {
		t.Errorf("comparison = %+v", cmp)
	}

	req := app.reqs[0]
	if n := countImages(req); n != 4 {
		t.Errorf("sent %d images, want front and side of first and latest", n)
	}
	text := allText(req)
	for _, want := range []string{"First check, 1 September 2026", "Latest check, 15 October 2026", "waist 96.5 cm", "Write in English"} {
		if !strings.Contains(text, want) {
			t.Errorf("request lacks %q:\n%s", want, text)
		}
	}
	if req.Temperature != nil || !strings.Contains(req.System, "No praise or criticism") {
		t.Error("the comparison must send no temperature and carry the prompt's tone rules")
	}

	saved, stale, ok, err := f.svc.SavedComparison(f.ctx, f.user)
	if err != nil || !ok || stale || saved.Summary != cmp.Summary || !saved.ToOn.Equal(date(2026, 10, 15)) {
		t.Fatalf("saved = %+v stale %v ok %v err %v", saved, stale, ok, err)
	}

	// The coach reads it, labelled as an observation.
	var into coach.Context
	if err := progresschecks.NewContextSource(f.svc).Collect(f.ctx, coach.ContextRequest{User: f.user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if !strings.Contains(strings.Join(into.FitnessSummary, "\n"), "observation from photos") {
		t.Errorf("coach context lacks the comparison: %v", into.FitnessSummary)
	}
}

func TestComparisonGoesStaleAndIsReplaced(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{compareReply}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))

	f.saveOn(t, date(2026, 9, 1), 100, progresscheck.Front)
	f.saveOn(t, date(2026, 10, 1), 98, progresscheck.Front)
	if _, err := f.svc.Compare(f.ctx, f.user); err != nil {
		t.Fatalf("compare: %v", err)
	}
	newest := f.saveOn(t, date(2026, 10, 15), 97, progresscheck.Side)

	_, stale, ok, err := f.svc.SavedComparison(f.ctx, f.user)
	if err != nil || !ok || !stale {
		t.Fatalf("after a newer check: stale %v ok %v err %v; want stale", stale, ok, err)
	}
	again, err := f.svc.Compare(f.ctx, f.user)
	if err != nil || again.ToCheckID != newest.ID {
		t.Fatalf("compare again: %+v, %v", again, err)
	}
	if _, stale, _, _ := f.svc.SavedComparison(f.ctx, f.user); stale {
		t.Error("the replaced comparison is still stale")
	}
}

func TestDeletingACheckDeletesItsComparison(t *testing.T) {
	f := newFixture(t)
	app := &stubClient{name: "anthropic", replies: []string{compareReply}}
	f.svc.WithAssist(progresschecks.NewAssist(nil, app))

	first := f.saveOn(t, date(2026, 9, 1), 100, progresscheck.Front)
	f.saveOn(t, date(2026, 10, 1), 98, progresscheck.Front)
	if _, err := f.svc.Compare(f.ctx, f.user); err != nil {
		t.Fatalf("compare: %v", err)
	}
	if err := f.svc.Delete(f.ctx, f.user, first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, ok, err := f.svc.SavedComparison(f.ctx, f.user); ok || err != nil {
		t.Errorf("the observation outlived its photos: ok %v err %v", ok, err)
	}
}

func TestCompareFailureStoresNothingAndNeverFailsOver(t *testing.T) {
	f := newFixture(t)
	own := &stubClient{name: "openai", err: errors.New("401 bad key")}
	app := &stubClient{name: "anthropic", replies: []string{compareReply}}
	f.svc.WithAssist(progresschecks.NewAssist(stubOwn{provider: "openai", client: own}, app))

	f.saveOn(t, date(2026, 9, 1), 100, progresscheck.Front)
	f.saveOn(t, date(2026, 10, 1), 98, progresscheck.Front)
	if _, err := f.svc.Compare(f.ctx, f.user); !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
	if app.calls() != 0 {
		t.Error("photos named for OpenAI went to Anthropic after a failure")
	}
	if _, _, ok, _ := f.svc.SavedComparison(f.ctx, f.user); ok {
		t.Error("a failed comparison was stored")
	}

	f.svc.WithAssist(nil)
	if _, err := f.svc.Compare(f.ctx, f.user); !errors.Is(err, progresschecks.ErrAssistUnavailable) {
		t.Errorf("no provider: err = %v", err)
	}
}
