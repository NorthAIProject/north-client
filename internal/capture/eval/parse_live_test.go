//go:build live

package eval_test

import (
	"os"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	aieval "github.com/NorthAIProject/north-client/internal/ai/eval"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/capture/eval"
)

// runs is how many times each case is put to the model.
//
// More than one because a model is not deterministic even at temperature zero,
// and a suite that goes red on a single flaky reply gets muted within a week.
// Small because every run is a paid call.
const runs = 3

// passRate is the share of runs that must satisfy every assertion.
//
// Not 1.0 deliberately. These cases grade instruction-following, and holding
// out for perfection on a sampled process produces a suite nobody trusts and
// everybody skips. A case that drops below this is a real regression; a case
// that fails once in three is the weather.
const passRate = 2.0 / 3.0

// TestParseLive runs the corpus against a real provider, through the
// production parser.
//
// It builds the real capture.AIParser over a runner holding the live client,
// so what is graded is the whole path — prompt, schema, decode, convert,
// coverage check — rather than a reimplementation of it that could drift.
//
// Costs money. Run with `task test:live`.
func TestParseLive(t *testing.T) {
	client := aieval.Provider(t)

	registry := ai.NewRegistry()
	registry.Register(client)
	runner := ai.NewRunner(registry, ai.NewChainSet([]string{client.Name()}, nil))

	// Empty model: the provider's own default, which eval.Provider has already
	// chosen and logged.
	parser := capture.NewAIParser(runner, os.Getenv("EVAL_MODEL"))

	// Counted across every case, because a run that graded almost nothing must
	// not report ok. Somebody who typed `task test:live` wanted an answer, and
	// a green tick over a mostly skipped suite is the same lie as a database
	// test that skips when its URL is unset.
	//
	// A threshold rather than "at least one": the first version of this guard
	// only caught a total blackout, and a run where eight of nine cases were
	// rate-limited away still came back green.
	var gradedCases, attemptedCases atomic.Int64

	for _, c := range eval.Cases() {
		t.Run(c.ID, func(t *testing.T) {
			// Counted here rather than in the loop above: a case filtered out
			// by `go test -run` never enters this body, and comparing against
			// the whole corpus would fail every deliberately narrowed run.
			attemptedCases.Add(1)

			var passed, graded, refused int

			for run := 1; run <= attempts(t); run++ {
				draft, err := parser.Parse(aieval.Context(t), c.User(), c.Text, c.Known())
				if err != nil {
					// A provider that timed out, rate-limited or fell over
					// says nothing about the prompt. Counting it as a failed
					// run would report "your prompt is wrong" for a network
					// problem, which is the fastest way to make an eval suite
					// something people ignore.
					refused++
					t.Logf("run %d: provider refused: %v", run, err)
					continue
				}

				graded++
				failures := c.GradeDraft(draft)
				if len(failures) == 0 {
					passed++
					continue
				}
				for _, failure := range failures {
					t.Logf("run %d: %s", run, failure)
				}
			}

			// Nothing was graded, so there is no result — not a pass and not a
			// failure. Skipping is what eval.Provider already does when a key
			// is absent, and the message names the cause so this cannot be
			// mistaken for the case having been checked.
			if graded == 0 {
				t.Skipf("no run reached the model (%d refused); the prompt was not graded", refused)
			}

			gradedCases.Add(1)

			if rate := float64(passed) / float64(graded); rate < passRate {
				t.Errorf("passed %d of %d graded runs (%.0f%%), want at least %.0f%% [%d refused by the provider]\nwhy this matters: %s",
					passed, graded, rate*100, passRate*100, refused, c.Why)
			}
		})
	}

	// Half is the line. Below it the run says more about the provider than
	// about the prompt, and reporting a pass would invite somebody to trust a
	// corpus that mostly did not execute.
	attempted, graded := int(attemptedCases.Load()), int(gradedCases.Load())
	if attempted > 0 && graded*2 < attempted {
		t.Fatalf("only %d of %d attempted cases reached the model; the corpus did not really run.\n"+
			"Free models are rate-limited per minute — try EVAL_RUNS=1, or a paid model.",
			graded, attempted)
	}
}

// attempts lets a run be widened when chasing a flaky case, without editing
// the constant that CI reads.
func attempts(t *testing.T) int {
	t.Helper()

	raw := os.Getenv("EVAL_RUNS")
	if raw == "" {
		return runs
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		t.Fatalf("EVAL_RUNS=%q is not a positive number", raw)
	}
	return n
}
