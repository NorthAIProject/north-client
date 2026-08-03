package media

import (
	"context"
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// recentAnalyses bounds how many form checks reach the coach's context.
const recentAnalyses = 3

// ContextSource lets the chat coach reference what it saw in the user's videos,
// so "why does my back round on squats?" can be answered from the footage
// rather than in the abstract.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "form-analyses" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	analyses, err := s.svc.ListAnalyses(ctx, req.User.ID, recentAnalyses)
	if err != nil {
		return err
	}

	for _, item := range analyses {
		if item.Result == nil || !item.Result.Trustworthy() {
			// An analysis the model could not read tells the coach nothing, and
			// passing it on invites it to treat a non-observation as a finding.
			continue
		}

		var b strings.Builder
		fmt.Fprintf(&b, "%s on %s: %s",
			item.Result.Exercise,
			item.CreatedAt.In(req.User.Location()).Format("2 Jan"),
			item.Result.Summary)

		for _, issue := range item.Result.Issues {
			fmt.Fprintf(&b, " [%s at %s: %s]", issue.Severity, issue.Timestamp(), issue.Observation)
		}

		into.FormAnalyses = append(into.FormAnalyses, b.String())
	}

	return nil
}

var _ coach.ContextSource = (*ContextSource)(nil)
