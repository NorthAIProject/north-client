package insights

import (
	"os"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/rasterize"
)

func TestDigestCardMirrorsThePage(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	card := digestCard(view)

	if card.Title != view.Range.Label {
		t.Errorf("title = %q, want the window's own label %q", card.Title, view.Range.Label)
	}
	if len(card.Rings) != len(view.Scores) {
		t.Errorf("%d rings for %d scores", len(card.Rings), len(view.Scores))
	}
	for i, r := range card.Rings {
		if r.Label != view.Scores[i].Label {
			t.Errorf("ring %d labelled %q, want %q", i, r.Label, view.Scores[i].Label)
		}
		if r.HasData != view.Scores[i].HasData {
			t.Errorf("ring %q HasData = %v, want %v", r.Label, r.HasData, view.Scores[i].HasData)
		}
		if r.Points != view.Scores[i].Points {
			t.Errorf("ring %q = %d points, want %d", r.Label, r.Points, view.Scores[i].Points)
		}
	}
}

func TestDigestCardSaysHowManyAreOnTrack(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	if got := digestCard(view).Subtitle; got == "" {
		t.Error("no subtitle; the card does not say how the window went")
	}
}

func TestDigestCardDrawsTheFirstPinnedSeries(t *testing.T) {
	// One series, not four: the card is read on a phone, and the rest of the
	// numbers are one tap away on the page it links to.
	view := mustSummaryView(t, wellLogged(t))

	card := digestCard(view)
	if len(card.Bars.Values) == 0 {
		t.Fatal("no bar series drawn")
	}
	if card.Bars.Label == "" {
		t.Error("the bar series is unlabelled")
	}
}

func TestDigestCardOfAnEmptyWindowDrawsNoBars(t *testing.T) {
	view := mustSummaryView(t, SummaryData{Range: weekRange(t)})

	card := digestCard(view)
	if len(card.Bars.Values) != 0 {
		t.Error("drew bars for a window with nothing in it")
	}
}

func TestDigestCardRenders(t *testing.T) {
	// The mapping is only useful if what it produces actually draws.
	view := mustSummaryView(t, wellLogged(t))

	raw, err := rasterize.RenderCard(digestCard(view))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(raw) == 0 {
		t.Error("rendered nothing")
	}
}

// TestWriteSampleDigestCard writes the card a real digest would carry.
//
// The mapping tests prove the right numbers reach the right rings; only
// looking at it proves the result is worth sending. Set DIGEST_SAMPLE to a
// path to write one.
func TestWriteSampleDigestCard(t *testing.T) {
	path := os.Getenv("DIGEST_SAMPLE")
	if path == "" {
		t.Skip("set DIGEST_SAMPLE to write a sample card")
	}

	view := mustSummaryView(t, wellLogged(t))
	raw, err := rasterize.RenderCard(digestCard(view))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %d bytes", len(raw))
}
