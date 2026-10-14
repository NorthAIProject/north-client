package memories

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderMemoriesIn(t *testing.T, locale users.Locale, pending, approved []memory.Memory) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	page := Page(users.User{DisplayName: "Ana", Locale: locale}, pending, approved, Instruments{}, MemoryForm{}, nil)
	if err := page.Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestMemoriesRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Known facts", "Add a fact", "Save fact"},
		users.LocalePTPT: {"Factos conhecidos", "Adicionar um facto", "Guardar facto"},
		users.LocalePTBR: {"Fatos conhecidos", "Adicionar um fato", "Salvar fato"},
		users.LocaleES:   {"Datos conocidos", "Añadir un dato", "Guardar dato"},
	}
	for locale, wants := range cases {
		html := renderMemoriesIn(t, locale, nil, nil)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// Facts are the person's words and stay as written; everything around them,
// including the category the fact is filed under, is translated.
func TestMemoryCardsTranslateEverythingButTheFact(t *testing.T) {
	pending := []memory.Memory{{ID: uuid.New(), Category: memory.CategoryInjury, Content: "Left knee clicks on deep squats"}}
	approved := []memory.Memory{{ID: uuid.New(), Category: memory.CategoryHabit, Content: "Trains before work", Pinned: true}}
	html := renderMemoriesIn(t, users.LocaleES, pending, approved)

	for _, want := range []string{
		"Left knee clicks on deep squats",
		"Trains before work",
		"Propuestos en conversaciones",
		"lesión",
		"hábito",
		"fijado",
		"Aprobar",
		"¿Eliminar este dato?",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("es: page does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"Known facts", "Approve", "Discard", "Delete this fact?", ">pinned<", "Hide from coach"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("es: page still contains the English %q", unwanted)
		}
	}
	// The value posted back is the stored category, not its label.
	if !strings.Contains(html, `value="injury"`) {
		t.Error("the category picker no longer posts the stored value")
	}
}
