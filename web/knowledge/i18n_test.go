package knowledge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents/document"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderIn(t *testing.T, locale users.Locale, view View) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	if err := IndexPage(users.User{DisplayName: "Ana", Locale: locale}, view, NoteForm{}).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestKnowledgeRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Add to the library", "Save note", "The coach has nothing to quote yet"},
		users.LocalePTPT: {"Adicionar à biblioteca", "Guardar nota", "O treinador ainda não tem nada para citar"},
		users.LocalePTBR: {"Adicionar à biblioteca", "Salvar nota", "O treinador ainda não tem nada para citar"},
		users.LocaleES:   {"Añadir a la biblioteca", "Guardar nota", "El entrenador aún no tiene nada que citar"},
	}
	for locale, wants := range cases {
		html := renderIn(t, locale, View{})
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// A library with a document carries the table, the badges and the meter: the
// parts most built from small strings, and so the likeliest to keep English.
func TestKnowledgeLibraryLeavesNoEnglishBehind(t *testing.T) {
	read := time.Date(2026, time.March, 4, 9, 0, 0, 0, time.UTC)
	view := View{
		Documents: []document.Document{{
			ID:         uuid.New(),
			Title:      "Physio report",
			SourceKind: document.SourceUpload,
			MIME:       "application/pdf",
			ByteSize:   142 * 1024,
			Status:     document.StatusReady,
			CreatedAt:  read,
			IndexedAt:  &read,
		}},
		Counts:       document.Counts{Ready: 1, Pending: 2, Chunks: 12},
		EmbeddingGap: 3,
	}
	html := renderIn(t, users.LocaleES, view)

	for _, want := range []string{
		"Physio report", // the person's title is never translated
		"Requiere atención",
		"3 pasajes se pueden buscar por palabras",
		"2 en espera",
		"Tamaño",
		"leído",
		"4 mar",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("es: page does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"Needs attention", "Rebuild index", "Remove", "4 Mar"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("es: page still contains the English %q", unwanted)
		}
	}
}

func TestSearchPageRendersInTheChosenLanguage(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(users.LocalePTPT))
	view := SearchView{Query: "grip", Hits: []document.Hit{{Title: "Physio notes", Content: "grip"}}}
	if err := SearchPage(users.User{DisplayName: "Ana"}, view).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if !strings.Contains(html, "Pesquisar passagens") || !strings.Contains(html, "1 passagem") {
		t.Errorf("pt-PT search page is not translated:\n%s", html)
	}
	if strings.Contains(html, "Search passages") {
		t.Error("pt-PT search page still carries the English heading")
	}
}
