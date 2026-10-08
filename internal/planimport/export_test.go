package planimport

import "net/http"

// Exported for the black-box handler test.
func (h *Handler) WorkoutParse(w http.ResponseWriter, r *http.Request)  { h.workoutParse(w, r) }
func (h *Handler) WorkoutReview(w http.ResponseWriter, r *http.Request) { h.workoutReview(w, r) }
