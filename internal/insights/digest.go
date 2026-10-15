package insights

import (
	"context"
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
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
	return renderDigest(string(user.Locale), view, s.siteURL), renderDigestCard(string(user.Locale), view), nil
}

// digestView loads the window and builds the view both the words and the
// picture are read from.
func (s *Service) digestView(ctx context.Context, user users.User, rg timerange.Range) (insightpages.SummaryView, error) {
	ctx = digestContext(ctx, user)
	data, err := s.Summary(ctx, user, rg)
	if err != nil {
		return insightpages.SummaryView{}, err
	}
	return buildSummaryView(ctx, data)
}

// digestContext carries the recipient's language.
//
// The scheduled digest runs from the worker with no request behind it, so
// nothing upstream has set a locale; /stats does arrive through a request, but
// the reader is the account holder either way. Taking the locale from the user
// rather than trusting the context makes both paths read the same.
func digestContext(ctx context.Context, user users.User) context.Context {
	return i18n.WithLocale(ctx, string(user.Locale))
}

// renderDigestCard draws the card, or returns nothing.
//
// A failure to draw is not a failure to send: the words carry the substance
// and the picture supports them, so a digest without its card is still worth
// delivering. The same position Send already takes on a refused illustration.
//
// The card's labels come from the view, already in tag's language; its
// subtitle is the one sentence digestCard writes itself, so it is reworded
// here in the recipient's language — the same words as the message above it.
func renderDigestCard(tag string, view insightpages.SummaryView) []byte {
	if view.Empty || view.Judged == 0 {
		return nil
	}
	card := digestCard(view)
	card.Subtitle = onTrack(tag, view)
	raw, err := rasterize.RenderCard(card)
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
//
// tag is the recipient's locale. The view's own words (range, domains,
// verdicts, highlights) are already in it, because digestView built the view
// under the same locale; the sentences written here are the only ones left.
// "On track" is the page's own key, so the digest still says what the page says.
func renderDigest(tag string, view insightpages.SummaryView, siteURL string) string {
	if view.Empty || view.Judged == 0 {
		// Both forms of the label go in: English reads it mid-sentence in lower
		// case, the others lead with it as a heading.
		return i18n.TranslateF(tag, "tg.digest.empty", strings.ToLower(view.Range.Label), view.Range.Label)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "*%s*\n", view.Range.Label)
	b.WriteString(onTrack(tag, view) + ".\n")

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

// onTrack is the page's "n of m on track", in tag's language.
func onTrack(tag string, view insightpages.SummaryView) string {
	return i18n.TranslateF(tag, "insights.summary.onTrack", view.OnTrack, view.Judged)
}
