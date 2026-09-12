package insights

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// SpendData is what this person's account spent on model calls over a window.
//
// Their own account only. The operator-facing view across every account stays
// where it was, behind the `web spend` command: a page that could be pointed
// at somebody else's usage is not a page this section should be able to draw.
type SpendData struct {
	Range timerange.Range

	Surfaces []spend.SurfaceSpend
	Models   []spend.ModelSpend
}

// Spend loads the window's generation cost, split two ways.
func (s *Service) Spend(ctx context.Context, user users.User, rg timerange.Range) (SpendData, error) {
	out := SpendData{Range: rg}
	if s.spend == nil {
		return out, nil
	}

	window := spend.Range{From: rg.Since, To: rg.Until}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		out.Surfaces, err = s.spend.UserSurfaces(gctx, user.ID, window)
		return
	})
	g.Go(func() (err error) {
		out.Models, err = s.spend.UserModels(gctx, user.ID, window)
		return
	})

	if err := g.Wait(); err != nil {
		return SpendData{}, err
	}
	return out, nil
}
