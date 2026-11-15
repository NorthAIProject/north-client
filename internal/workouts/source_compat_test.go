package workouts

import (
	"testing"

	"github.com/google/uuid"
)

// The bug this exists for: a plan imported from a file is stored with source
// "imported", but the iOS app decodes PlanSummary.source as a closed enum of
// ai and edited. One imported plan made the whole plan list undecodable, and
// the Training tab said "Training did not load" for everyone who had imported
// a plan on the web.
//
// Shipped apps cannot be fixed from here except by never sending them a value
// they do not know. So source stays within ai/edited, and the full provenance
// moves to origin, an open string a client can add cases to at its own pace.
func TestImportedPlansStayDecodableByShippedClients(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		stored, source, origin string
	}{
		{"", SourceAI, SourceAI},
		{SourceAI, SourceAI, SourceAI},
		{SourceEdited, SourceEdited, SourceEdited},
		{SourceImported, SourceEdited, SourceImported},
	} {
		p := contractPlan()
		p.Source = tt.stored

		summary := projectSummary(p)
		if summary.Source != tt.source || summary.Origin != tt.origin {
			t.Errorf("summary of a %q plan: source %q origin %q, want %q %q",
				tt.stored, summary.Source, summary.Origin, tt.source, tt.origin)
		}

		detail := projectDetail(p, nil, WeekProgress{}, uuid.Nil)
		if detail.Source != tt.source || detail.Origin != tt.origin {
			t.Errorf("detail of a %q plan: source %q origin %q, want %q %q",
				tt.stored, detail.Source, detail.Origin, tt.source, tt.origin)
		}
	}
}
