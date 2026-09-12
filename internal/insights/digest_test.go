package insights

import (
	"strings"
	"testing"

	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func TestDigestNamesEveryScoredDomain(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	got := renderDigest(view, "https://north.test")

	for _, want := range []string{"Body", "Mind", "Training"} {
		if !strings.Contains(got, want) {
			t.Errorf("digest does not mention %s:\n%s", want, got)
		}
	}
}

func TestDigestSaysTheSameWordsAsThePage(t *testing.T) {
	// The whole reason this is deterministic: a digest that disagreed with the
	// page it links to would make both untrustworthy, and the reader has no
	// way to tell which one is lying.
	view := mustSummaryView(t, wellLogged(t))

	got := renderDigest(view, "https://north.test")

	for _, s := range view.Scores {
		if !s.HasData {
			continue
		}
		if !strings.Contains(got, s.Verdict) {
			t.Errorf("page says %s is %q; digest does not:\n%s", s.Label, s.Verdict, got)
		}
	}
	for _, h := range view.Highlights {
		if !strings.Contains(got, h) {
			t.Errorf("page highlight %q is missing from the digest:\n%s", h, got)
		}
	}
}

func TestDigestSkipsDomainsWithNothingLogged(t *testing.T) {
	// A line reading "Nutrition — no data" every week is noise somebody
	// learns to skip, and it teaches them to skip the rest with it.
	view := mustSummaryView(t, wellLogged(t))

	got := renderDigest(view, "https://north.test")

	if strings.Contains(got, "Nutrition") {
		t.Errorf("digest mentions an unscored domain:\n%s", got)
	}
}

func TestDigestLinksToTheWindowItDescribes(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	got := renderDigest(view, "https://north.test")

	want := "https://north.test/app/insights?range=" + view.Range.Key
	if !strings.Contains(got, want) {
		t.Errorf("digest does not link to %q:\n%s", want, got)
	}
}

func TestDigestWithoutASiteURLStillReads(t *testing.T) {
	// A deployment with no base URL configured must send the numbers rather
	// than a message ending in a broken link.
	view := mustSummaryView(t, wellLogged(t))

	got := renderDigest(view, "")

	if strings.Contains(got, "http") {
		t.Errorf("digest invented a link:\n%s", got)
	}
	if !strings.Contains(got, "Body") {
		t.Errorf("digest lost its content along with the link:\n%s", got)
	}
}

func TestDigestOfAnEmptyWindowSaysSoInOneLine(t *testing.T) {
	view := mustSummaryView(t, SummaryData{Range: weekRange(t)})

	got := renderDigest(view, "https://north.test")

	if got == "" {
		t.Fatal("empty window produced no message at all")
	}
	if strings.Count(got, "\n") > 2 {
		t.Errorf("an empty window should be one line, got:\n%s", got)
	}
}

func TestDigestIsPlainEnoughForTelegram(t *testing.T) {
	// The client renders light Markdown and falls back to stripping it. A
	// table or a heading would survive neither path.
	view := mustSummaryView(t, wellLogged(t))

	got := renderDigest(view, "https://north.test")

	for _, banned := range []string{"|", "#", "```"} {
		if strings.Contains(got, banned) {
			t.Errorf("digest contains %q, which Telegram will not render:\n%s", banned, got)
		}
	}
}

var _ = insightpages.SummaryView{}
