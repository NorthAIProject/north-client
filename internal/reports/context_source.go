package reports

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/coach"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ContextSource puts the latest ready weekly review in front of the coach.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "reports" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	report, err := s.svc.LatestReady(ctx, req.User.ID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return nil
		}
		return err
	}
	into.Reports = append(into.Reports, report.Summary())
	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
