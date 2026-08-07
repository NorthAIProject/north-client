package meals

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	nutritionpages "github.com/NorthAIProject/north-client/web/nutrition"
)

func (h *Handler) ingredientsIndex(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	results, err := h.ingredients.Search(r.Context(), user.ID, query, 50)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, nutritionpages.IngredientsPage(user, query, results, nutritionpages.IngredientForm{}))
}

func (h *Handler) createIngredient(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := nutritionpages.IngredientForm{
		Name: r.PostFormValue("name"), Brand: r.PostFormValue("brand"), Category: r.PostFormValue("category"),
		ServingSizeGrams: r.PostFormValue("serving_size_grams"),
		Calories:         r.PostFormValue("calories_per_100g"),
		ProteinG:         r.PostFormValue("protein_g_per_100g"),
		FatG:             r.PostFormValue("fat_g_per_100g"),
		CarbG:            r.PostFormValue("carbs_g_per_100g"),
	}

	in := IngredientInput{
		Name: form.Name, Brand: form.Brand, Category: form.Category,
		ServingSizeGrams: parseFloat(form.ServingSizeGrams),
		Per100g: Macros{
			Calories: parseFloat(form.Calories), ProteinG: parseFloat(form.ProteinG),
			FatG: parseFloat(form.FatG), CarbG: parseFloat(form.CarbG),
		},
	}

	if _, err := h.ingredients.Create(r.Context(), user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			results, listErr := h.ingredients.Search(r.Context(), user.ID, "", 50)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			h.render(w, r, http.StatusUnprocessableEntity, nutritionpages.IngredientsPage(user, "", results, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/ingredients", http.StatusSeeOther)
}

func (h *Handler) deleteIngredient(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.ingredients.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/ingredients", http.StatusSeeOther)
}

// parseFloat treats an unparseable or empty value as zero — every numeric
// field here is optional except calories, which the service validates as
// required (>=0) separately.
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}
