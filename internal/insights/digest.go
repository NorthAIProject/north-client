package insights

import (
	"context"
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/rasterize"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// Digest renders this person's window as a message.
//
// Built from the same view the web page renders, so the two cannot drift: a
// digest that disagreed with the page it links to would make both untrustworthy,
// and the reader has no way to tell which one is lying.
//
// No model call. The scores and the highlights are already deterministic, so a
// digest costs a handful of queries rather than a generation — which is what
// lets somebody set it to daily without that being an expensive thing to ask
// for.
func (s *Service) Digest(ctx context.Context, user users.User, rg timerange.Range) (string, []byte, error) {
	view, err := s.digestView(ctx, user, rg)
	if err != nil {
		return "", nil, err
	}
	return renderDigest(view, s.siteURL), renderDigestCard(view), nil
}

// digestView loads the window and builds the view both the words and the
// picture are read from.
func (s *Service) digestView(ctx context.Context, user users.User, rg timerange.Range) (insightpages.SummaryView, error) {
	data, err := s.Summary(ctx, user, rg)
	if err != nil {
		return insightpages.SummaryView{}, err
	}
	return buildSummaryView(data)
}

// renderDigestCard draws the card, or returns nothing.
//
// A failure to draw is not a failure to send: the words carry the substance
// and the picture supports them, so a digest without its card is still worth
// delivering. The same position Send already takes on a refused illustration.
func renderDigestCard(view insightpages.SummaryView) []byte {
	if view.Empty || view.Judged == 0 {
		return nil
	}
	raw, err := rasterize.RenderCard(digestCard(view))
	if err != nil {
		return nil
	}
	return raw
}

// renderDigest writes the message.
//
// Plain text with light bold and nothing else. The Telegram client converts
// Markdown to HTML and, when the platform rejects it, retries with the markup
// stripped — so a table or a heading would survive neither path.
func renderDigest(view insightpages.SummaryView, siteURL string) string {
	if view.Empty || view.Judged == 0 {
		return "Nothing logged " + strings.ToLower(view.Range.Label) + " yet."
	}

	var b strings.Builder

	fmt.Fprintf(&b, "*%s*\n", view.Range.Label)
	fmt.Fprintf(&b, "%d of %d on track.\n", view.OnTrack, view.Judged)

	for _, s := range view.Scores {
		// An unscored domain is left out rather than printed as "no data".
		// A line that says nothing every week teaches people to skim past the
		// lines that do.
		if !s.HasData {
			continue
		}
		fmt.Fprintf(&b, "\n%s — %s (%d)", s.Label, s.Verdict, s.Points)
	}

	if len(view.Highlights) > 0 {
		b.WriteString("\n")
		for _, h := range view.Highlights {
			fmt.Fprintf(&b, "\n• %s", h)
		}
	}

	if siteURL != "" {
		fmt.Fprintf(&b, "\n\n%s/app/insights?range=%s", strings.TrimRight(siteURL, "/"), view.Range.Key)
	}

	return b.String()
}
