package workouts

import (
	"context"
	"strings"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// importProvider fills workout_plans.provider for an imported plan. The column
// records which generation a plan came from; for an import there was none.
const importProvider = "import"

// ImportPlan stores a plan the person read in from a file and confirmed.
//
// It is stored exactly like a generated one — same JSONB shape, same
// versioning, same edit routes — so nothing downstream needs to know where it
// came from. The differences are the provenance columns and the placeholder
// intake row (see CreateImportedIntake).
func (s *Service) ImportPlan(ctx context.Context, user users.User, p Plan) (StoredPlan, error) {
	p.Name = strings.TrimSpace(p.Name)
	for i := range p.Days {
		// Stored in the generator's spelling, which NextSession and the phone
		// both compare against.
		if wd, ok := parseImportedWeekday(p.Days[i].Weekday); ok {
			p.Days[i].Weekday = wd
		}
	}

	if problems := plan.ValidateImported(p); len(problems) > 0 {
		return StoredPlan{}, apperr.Wrap(apperr.ErrValidation, "%s", strings.Join(problems, "; "))
	}

	intake, err := s.repo.CreateImportedIntake(ctx, user.ID, len(p.Days))
	if err != nil {
		return StoredPlan{}, err
	}

	return s.repo.CreatePlan(ctx, StoredPlan{
		UserID:   user.ID,
		IntakeID: intake.ID,
		Plan:     p,
		Provider: importProvider,
		Source:   SourceImported,
	})
}

func parseImportedWeekday(label string) (string, bool) {
	label = strings.TrimSpace(label)
	for _, d := range []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"} {
		if strings.EqualFold(d, label) {
			return d, true
		}
	}
	return "", false
}
