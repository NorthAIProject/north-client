package agent

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
)

// The pullup as the catalogue actually holds it: one of only 33 rows carrying
// artwork, a video and written cues at once.
func richPullUp() exercise.Exercise {
	return exercise.Exercise{
		Slug:             "pull-up",
		Name:             "Pull-up",
		Category:         "strength",
		Difficulty:       "intermediate",
		Equipment:        "pull-up bar",
		Primary:          []string{"lats"},
		Secondary:        []string{"biceps", "forearms"},
		Instructions:     "Hang from the bar with an overhand grip. Pull until your chin clears it.",
		VideoURL:         "https://youtu.be/HVgBWSOPx0I",
		IllustrationSlug: "pull-up",
	}
}

// get_exercise held both of these and printed neither, so no model on any
// channel could show anyone the movement.
func TestDescribeExerciseHandsBackTheVideoAndTheIllustration(t *testing.T) {
	t.Parallel()

	got := describeExercise(richPullUp(), "https://kheprios.com")

	for _, want := range []string{
		"Pull-up (strength, intermediate)",
		"Equipment: pull-up bar",
		"Primary muscles: lats",
		"Secondary muscles: biceps, forearms",
		"How to perform it: Hang from the bar",
		"Video: https://youtu.be/HVgBWSOPx0I",
		"Illustration: https://kheprios.com/assets/exercises/pull-up/frame-1.svg",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// 269 of 455 rows have artwork and no text at all. The model must be told that
// plainly, because the alternative is it inventing form advice — the one
// failure mode here with physical consequences.
func TestAnExerciseWithNoInstructionsSaysSo(t *testing.T) {
	t.Parallel()

	thin := exercise.Exercise{
		Name: "Wall Walk", Category: "strength", Difficulty: "advanced",
		Equipment: "none", Primary: []string{"shoulders"},
		IllustrationSlug: "wall-walk",
	}

	got := describeExercise(thin, "https://kheprios.com")

	if !strings.Contains(got, "not recorded in the catalogue") {
		t.Errorf("the gap is not stated:\n%s", got)
	}
	if strings.Contains(got, "Video:") {
		t.Errorf("a video line appeared for a row with no video:\n%s", got)
	}
	if !strings.Contains(got, "/assets/exercises/wall-walk/frame-1.svg") {
		t.Errorf("the illustration it does have was withheld:\n%s", got)
	}
}

// The origin is environment-specific: an agent talking to a laptop should be
// given that laptop's addresses.
func TestTheIllustrationURLFollowsTheConfiguredOrigin(t *testing.T) {
	t.Parallel()

	got := describeExercise(richPullUp(), "http://localhost:8090")
	if !strings.Contains(got, "http://localhost:8090/assets/exercises/pull-up/frame-1.svg") {
		t.Errorf("did not use the configured origin:\n%s", got)
	}
}

// A trailing slash on BASE_URL is ordinary and must not produce a doubled one.
func TestATrailingSlashOnTheOriginDoesNotDoubleUp(t *testing.T) {
	t.Parallel()

	got := describeExercise(richPullUp(), "https://kheprios.com/")
	if strings.Contains(got, "com//assets") {
		t.Errorf("doubled slash in the asset URL:\n%s", got)
	}
}

// With no origin configured there is no absolute URL to give, and half a URL is
// worse than none.
func TestNoOriginMeansNoIllustrationLine(t *testing.T) {
	t.Parallel()

	got := describeExercise(richPullUp(), "")
	if strings.Contains(got, "Illustration:") {
		t.Errorf("emitted an illustration line with no origin to build it from:\n%s", got)
	}
	if !strings.Contains(got, "Video:") {
		t.Errorf("the video is absolute already and should still be there:\n%s", got)
	}
}
