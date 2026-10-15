package providers_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
)

// Every note the settings page shows has a translation, and English reads
// exactly what the catalogue says.
func TestEveryProviderNoteIsTranslated(t *testing.T) {
	for _, p := range providers.Catalog {
		if p.Note == "" {
			if got := p.NoteIn("es"); got != "" {
				t.Errorf("%s has no note but NoteIn invented %q", p.Name, got)
			}
			continue
		}
		if got := p.NoteIn("en"); got != p.Note {
			t.Errorf("%s: English note %q drifted from the catalogue's %q", p.Name, got, p.Note)
		}
		for _, tag := range []string{"es", "pt-PT", "pt-BR"} {
			if got := p.NoteIn(tag); got == p.Note || got == "" {
				t.Errorf("%s: %s note is %q", p.Name, tag, got)
			}
		}
	}
}
