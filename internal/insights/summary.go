package insights

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// SummaryData is every domain at once, for the page that opens the section.
//
// It adds no query of its own: the four loaders below already assemble exactly
// what the four detail pages show, and a summary that read the data a second
// way would eventually disagree with the page it links to.
type SummaryData struct {
	Range timerange.Range

	Body      BodyData
	Mind      MindData
	Progress  ProgressData
	Training  TrainingData
	Nutrition NutritionData
}

// Summary loads every domain for a window.
//
// The four loaders each fan out internally, so this is a fan-out of fan-outs.
// That is the point: the page is four pages' worth of data and would take four
// times as long run in sequence.
func (s *Service) Summary(ctx context.Context, user users.User, rg timerange.Range) (SummaryData, error) {
	out := SummaryData{Range: rg}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() (err error) {
		out.Body, err = s.Body(gctx, user, rg)
		return
	})
	g.Go(func() (err error) {
		out.Mind, err = s.Mind(gctx, user, rg)
		return
	})
	g.Go(func() (err error) {
		out.Progress, err = s.Progress(gctx, user, rg)
		return
	})
	g.Go(func() (err error) {
		out.Training, err = s.Training(gctx, user, rg)
		return
	})
	g.Go(func() (err error) {
		out.Nutrition, err = s.Nutrition(gctx, user, rg)
		return
	})

	if err := g.Wait(); err != nil {
		return SummaryData{}, err
	}
	return out, nil
}
