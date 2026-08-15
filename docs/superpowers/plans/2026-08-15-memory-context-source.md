# Memory ContextSource and Prompt Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make approved profile facts reach the coach as a bounded, citable bullet list, and make the system prompt treat those lines as known facts while admitting gaps.

**Architecture:** Do not rebuild the memory slice. `memories.ContextSource` already follows the goals/check-ins shape (`Name` + `Collect` into `coach.Context`, registered in `cmd/web/main.go` and `cmd/mcp-server/main.go`). Retrieval already prefers pinned facts and ranks the rest against `req.Query` (falling back to newest approved when there is no query). This plan adds the missing hard character budget at Collect time, locks the Linear acceptance with check-ins-style source tests, and makes the "known facts / admit gaps" policy explicit in `coach_system.md`.

**Tech Stack:** Go, sqlc/Postgres via `testdb.New(t)`, Templ-free (no UI), `internal/ai/prompts` for the system prompt, `coach.ContextBuilder` fail-soft loop.

**Spec:** Linear issue "Memory ContextSource and prompt policy" in `docs/linear/north-feature-ready-os.csv`. Coach contract in `DOMAIN.md` § "How the coach sees all of this". Existing source pattern: `internal/goals/context_source.go` and `internal/checkins/context_source.go` + `internal/checkins/context_source_test.go`.

## Global Constraints

- Tests hit real Postgres through `testdb.New(t)`. No repository mocks. No testify.
- A `ContextSource` reports failure by returning an error; it must not swallow a failed query into an empty section. The builder (`internal/coach/context_builder.go`) logs and continues — that is how "source failure does not fail coach reply" is already implemented.
- Empty *sections* stay labelled in `Context.Render()` (`"Known about them: none recorded yet"`). "Empty memory is silent/graceful" means Collect returns `nil` with an empty slice, not that the heading is omitted. Silence in the prompt invites the model to invent personal facts; `DOMAIN.md` forbids that.
- Memories stay `[]coach.Evidence` with `coach.MemoryRef(id)`. Do not regress to bare strings. Do not drop ranked search.
- Prompts live only in `internal/ai/prompts/*.md`. Do not embed prompt text in handlers, services, or templates.
- Do not change SQL, migrations, extraction, the memories UI, or wiring in `cmd/web/main.go` / `cmd/mcp-server/main.go` unless a test proves they are wrong.
- Reuse `seedUser` from `internal/memories/service_test.go` (same `memories_test` package).
- Run tests with `go test` on the touched packages. `task test` is `go test ./...`.

---

## Current state (do not rebuild)

These already exist and already satisfy most of the Linear ticket. Executors must read them before editing.

| Piece | Where | What it already does |
| --- | --- | --- |
| Store | `internal/memories/` + `migrations/00010_create_user_memories.sql` | Approved / pending / rejected, pin, exclude, 240-char content cap |
| Retrieval | `Service.ForContext` (`service.go:125-153`) | Query → `SearchApprovedForContext` (pinned OR match). No query → `ListApprovedForContext` (pinned then newest). Limit 20. Pending/excluded never returned |
| Source | `internal/memories/context_source.go` | `Name() == "memories"`. Collect fills `into.Memories` as Evidence. Compile-time `var _ coach.ContextSource` |
| Wiring | `cmd/web/main.go:433`, `cmd/mcp-server/main.go:247` | `memories.NewContextSource(memorySvc)` is already registered |
| Render | `coach.Context.Render` → `evidenceSection` | Bullets: `- [[memory:<uuid>]] [category] content`. Empty: `Known about them: none recorded yet` |
| Fail-soft | `ContextBuilder.Build` + `TestAFailingSourceDoesNotAbortTheOthers` | Source error is logged; Build still returns the rest |
| Prompt | `internal/ai/prompts/coach_system.md` | Grounding rules 1–2: never invent, admit gaps. Rule 7: cite `[[memory:…]]` |
| Prompt tests | `prompts_test.go` `TestCoachPromptStatesGroundingRules` | Locks the invent/admit phrases |
| Live eval | `internal/ai/eval/grounding_live_test.go` | Optional, `-tags live`. Not part of this plan |

**The actual gaps this plan closes:**

1. No hard character budget. Twenty 240-char facts can dump ~5k characters into the system prompt.
2. No `context_source_test.go`. Check-ins have one; memories do not. The Linear acceptance is untested at the source.
3. Prompt never names "Known about them" as the known-facts list. Rule 1 is generic; the ticket wants the memory policy explicit.
4. `TestSystemPromptCarriesRulesAndContext` does not assert the memories heading.

---

## File map

- Create: `internal/memories/context_source_test.go` — Linear acceptance at the source, plus the budget tests
- Modify: `internal/memories/context_source.go` — `clipToBudget` after `ForContext`
- Modify: `internal/ai/prompts/coach_system.md` — one new grounding rule
- Modify: `internal/ai/prompts/prompts_test.go` — lock the new phrase
- Modify: `internal/coach/context_builder_test.go` — memories heading, filled and empty
- Modify: `internal/coach/service_test.go` — system prompt carries the memories empty label and the new rule

No new files under `cmd/`, no SQL, no UI.

---

### Task 1: Failing source tests (acceptance + budget)

**Files:**
- Create: `internal/memories/context_source_test.go`
- Test: `internal/memories/context_source_test.go`

**Interfaces:**
- Consumes: `memories.NewService`, `memories.NewContextSource`, `memories.Service.Create` / `SetPinned` / `InsertExtractions`, `seedUser` (already in `service_test.go`), `coach.Context` / `ContextRequest` / `MemoryRef`, `testdb.New`
- Produces: failing tests. `TestContextSourceHonoursCharBudget` and `TestContextSourcePrefersPinnedWithinBudget` must fail until Task 2. The fill / empty / error / pending tests describe current behaviour and should pass against today's Collect.

- [ ] **Step 1: Write `internal/memories/context_source_test.go`**

Copy the check-ins source-test shape from `internal/checkins/context_source_test.go`. Do not import checkins. Same package as the existing memory tests (`package memories_test`) so `seedUser` is in scope.

```go
package memories_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// contextCharBudget is the hard cap Task 2 will enforce. The test names the
// number so a silent change of the constant fails here rather than in prod.
const contextCharBudget = 2000

func TestContextSourceFillsMemories(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-fill@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	m, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryInjury,
		Content:  "Left knee is sore on deep squats",
	})
	if err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) != 1 {
		t.Fatalf("want 1 memory in context, got %d", len(into.Memories))
	}
	got := into.Memories[0]
	if got.Ref != coach.MemoryRef(m.ID) {
		t.Errorf("Ref = %q, want %q", got.Ref, coach.MemoryRef(m.ID))
	}
	if got.Label != "profile fact" {
		t.Errorf("Label = %q, want %q", got.Label, "profile fact")
	}
	if !strings.Contains(got.Text, "Left knee is sore on deep squats") {
		t.Errorf("Text missing the stored fact: %q", got.Text)
	}
	if !strings.Contains(got.Text, "[injury]") {
		t.Errorf("Text should carry the category, got %q", got.Text)
	}

	rendered := into.Render()
	if !strings.Contains(rendered, "Known about them:") {
		t.Fatalf("rendered context missing the memories heading:\n%s", rendered)
	}
	wantLine := "- [[" + got.Ref + "]] " + got.Text
	if !strings.Contains(rendered, wantLine) {
		t.Fatalf("memory should render as a cited bullet:\nwant %q\n\n%s", wantLine, rendered)
	}
}

func TestContextSourceStaysEmptyWhenNone(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "source-empty@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(context.Background(), coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) != 0 {
		t.Fatalf("empty store must leave Memories untouched, got %#v", into.Memories)
	}
	if !strings.Contains(into.Render(), "Known about them: none recorded yet") {
		t.Fatalf("empty section must be labelled, not omitted:\n%s", into.Render())
	}
}

func TestContextSourceSurfacesErrors(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-fail@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	pool.Close()

	into := &coach.Context{User: user}
	err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into)
	if err == nil {
		t.Fatal("a failing query must be reported, not swallowed into an empty section")
	}
	if len(into.Memories) != 0 {
		t.Fatalf("a failed collect should leave the section untouched, got %#v", into.Memories)
	}
}

func TestContextSourceName(t *testing.T) {
	t.Parallel()
	if got := memories.NewContextSource(nil).Name(); got != "memories" {
		t.Fatalf("Name() = %q, want %q", got, "memories")
	}
}

func TestContextSourceOmitsPendingAndExcluded(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-filter@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	if _, err := svc.InsertExtractions(ctx, user.ID, uuid.Nil, []extract.Candidate{
		{Category: memory.CategoryHabit, Content: "Sleeps by 22:00 on weeknights", Confidence: 0.9},
	}); err != nil {
		t.Fatal(err)
	}
	hidden, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryInjury,
		Content:  "Cannot do overhead pressing due to shoulder instability",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetExcluded(ctx, hidden.ID, user.ID, true); err != nil {
		t.Fatal(err)
	}
	kept, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryHabit,
		Content:  "Trains before work most weekdays",
	})
	if err != nil {
		t.Fatal(err)
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{
		User:  user,
		Query: "shoulder overhead press weekday training",
	}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) != 1 {
		t.Fatalf("want only the approved, non-excluded fact, got %d", len(into.Memories))
	}
	if into.Memories[0].Ref != coach.MemoryRef(kept.ID) {
		t.Fatalf("kept the wrong fact: %+v", into.Memories[0])
	}
}

func TestContextSourceHonoursCharBudget(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-budget@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	const body = "This is a durable profile fact written long enough that a handful of them overflow the prompt budget for memories."
	if utf8.RuneCountInString(body) < 80 {
		t.Fatal("fixture is too short to overflow the budget")
	}

	for i := range 16 {
		if _, err := svc.Create(ctx, user.ID, memories.Input{
			Category: memories.CategoryGeneral,
			Content:  fmt.Sprintf("%s (%02d extra)", body, i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(into.Memories) == 0 {
		t.Fatal("budget must keep some facts, not drop the section")
	}
	if len(into.Memories) >= 16 {
		t.Fatalf("budget did not drop anything, got %d facts", len(into.Memories))
	}

	used := 0
	for _, e := range into.Memories {
		used += utf8.RuneCountInString(e.Text)
	}
	if used > contextCharBudget {
		t.Fatalf("memory section is %d runes, budget is %d", used, contextCharBudget)
	}

	rendered := into.Render()
	if !strings.Contains(rendered, "Known about them:\n- [[memory:") {
		t.Fatalf("budgeted facts must still render as cited bullets:\n%s", rendered)
	}
}

func TestContextSourcePrefersPinnedWithinBudget(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "source-pinned-budget@north.test")
	svc := memories.NewService(memories.NewRepository(pool))

	const pinned = "Vegetarian, will not eat fish either"
	pin, err := svc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryConstraint,
		Content:  pinned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetPinned(ctx, pin.ID, user.ID, true); err != nil {
		t.Fatal(err)
	}

	const filler = "Unrelated background detail written long enough that many of them crowd the pinned fact out of a naive recency window."
	for i := range 16 {
		if _, err := svc.Create(ctx, user.ID, memories.Input{
			Category: memories.CategoryGeneral,
			Content:  fmt.Sprintf("%s (%02d)", filler, i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	into := &coach.Context{User: user}
	if err := memories.NewContextSource(svc).Collect(ctx, coach.ContextRequest{
		User:  user,
		Query: "how heavy should my deadlift warm-up sets be",
	}, into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	found := false
	used := 0
	for _, e := range into.Memories {
		used += utf8.RuneCountInString(e.Text)
		if e.Ref == coach.MemoryRef(pin.ID) {
			found = true
			if !strings.Contains(e.Text, "pinned") {
				t.Errorf("pinned fact should be labelled pinned, got %q", e.Text)
			}
		}
	}
	if !found {
		t.Fatal("a pinned fact must keep its seat inside the budget")
	}
	if used > contextCharBudget {
		t.Fatalf("memory section is %d runes, budget is %d", used, contextCharBudget)
	}
}
```

- [ ] **Step 2: Run the new tests and confirm the budget ones fail**

Run:

```bash
go test ./internal/memories/ -count=1 -run 'TestContextSource'
```

Expected:
- `TestContextSourceFillsMemories` PASS
- `TestContextSourceStaysEmptyWhenNone` PASS
- `TestContextSourceSurfacesErrors` PASS
- `TestContextSourceName` PASS
- `TestContextSourceOmitsPendingAndExcluded` PASS
- `TestContextSourceHonoursCharBudget` FAIL — all 16 facts are collected (`budget did not drop anything`)
- `TestContextSourcePrefersPinnedWithinBudget` may PASS today (search already keeps pinned). That is fine; Task 2 must not break it.

If the fill/empty/error tests fail, stop and inspect `context_source.go` before adding budget code — the existing source is the foundation.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/memories/context_source_test.go
git commit -m "$(cat <<'EOF'
test: lock memory ContextSource acceptance and budget

The Linear ticket needs source-level proof that approved facts reach
the coach, empty is labelled, failures surface, and the prompt section
cannot grow without a cap.
EOF
)"
```

---

### Task 2: Hard character budget in Collect

**Files:**
- Modify: `internal/memories/context_source.go`
- Test: `internal/memories/context_source_test.go` (already written)

**Interfaces:**
- Consumes: `Service.ForContext(ctx, userID, query) ([]Retrieved, error)` — unchanged. List arrives pinned-first.
- Produces: `clipToBudget(list []Retrieved, budget int) []Retrieved` used by Collect. Exported constant `ContextCharBudget = 2000` so the test can stay in `memories_test` without duplicating a magic number *or* keep the test-local const in sync. Prefer exporting the const from `memories` so the test imports it.

- [ ] **Step 1: Export the budget and clip after retrieval**

Replace `internal/memories/context_source.go` with:

```go
package memories

import (
	"context"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/coach"
)

// ContextCharBudget is the hard cap, in runes of Summary() text, on how much
// memory the coach is allowed to see in one turn.
//
// Twenty 240-character facts would otherwise dump ~5k characters into every
// system prompt. Pinned facts claim the budget first because the list from
// ForContext is already pinned-then-ranked (or pinned-then-newest).
const ContextCharBudget = 2000

// ContextSource puts approved profile facts in front of the coach.
//
// Which facts depends on what the user just said: the source ranks against
// req.Query and falls back to the newest facts only when there is no query to
// rank against. Pinned facts are included either way — see
// SearchApprovedForContext. Collect then clips to ContextCharBudget.
type ContextSource struct {
	svc *Service
}

func NewContextSource(svc *Service) *ContextSource { return &ContextSource{svc: svc} }

func (s *ContextSource) Name() string { return "memories" }

func (s *ContextSource) Collect(ctx context.Context, req coach.ContextRequest, into *coach.Context) error {
	list, err := s.svc.ForContext(ctx, req.User.ID, req.Query)
	if err != nil {
		return err
	}
	for _, m := range clipToBudget(list, ContextCharBudget) {
		into.Memories = append(into.Memories, coach.Evidence{
			Ref:     coach.MemoryRef(m.ID),
			Text:    m.Summary(),
			Label:   "profile fact",
			Snippet: m.Snippet,
		})
	}
	return nil
}

// clipToBudget keeps whole facts until the rune budget is spent.
//
// Facts are never truncated: a half-sentence in the prompt is worse than an
// omitted one. A fact that does not fit is skipped so a later shorter one can
// still take the leftover. Pinned facts arrive first, so they get first claim.
func clipToBudget(list []Retrieved, budget int) []Retrieved {
	if budget <= 0 || len(list) == 0 {
		return nil
	}
	out := make([]Retrieved, 0, len(list))
	used := 0
	for _, m := range list {
		n := utf8.RuneCountInString(m.Summary())
		if n > budget && len(out) == 0 && used == 0 {
			// A single fact larger than the whole budget still goes in.
			// Validate caps content at 240, so this is defensive only.
			out = append(out, m)
			break
		}
		if used+n > budget {
			continue
		}
		out = append(out, m)
		used += n
	}
	return out
}

var _ coach.ContextSource = (*ContextSource)(nil)
```

- [ ] **Step 2: Point the test at the exported constant**

In `context_source_test.go`, delete the local `const contextCharBudget = 2000` and use `memories.ContextCharBudget` everywhere that name appeared (`used > contextCharBudget` becomes `used > memories.ContextCharBudget`).

- [ ] **Step 3: Run the source tests**

```bash
go test ./internal/memories/ -count=1 -run 'TestContextSource|TestSearch|TestCreateApproved|TestPendingNotInContext|TestExcluded'
```

Expected: all PASS. In particular:
- `TestContextSourceHonoursCharBudget` — fewer than 16 facts, rune sum ≤ 2000
- `TestContextSourcePrefersPinnedWithinBudget` — pinned fact still present
- `TestSearchFindsAFactRecencyWouldHaveMissed` — still finds the buried injury when queried (Collect is not on this path; ForContext is unchanged)
- `TestSearchAlwaysKeepsPinnedFacts` — unchanged

- [ ] **Step 4: Commit**

```bash
git add internal/memories/context_source.go internal/memories/context_source_test.go
git commit -m "$(cat <<'EOF'
feat: cap coach memory context at 2000 runes

Pinned and ranked facts still load; Collect now drops whole bullets
once the section would overflow, so the system prompt stays bounded.
EOF
)"
```

---

### Task 3: Prompt policy — known facts, admit gaps

**Files:**
- Modify: `internal/ai/prompts/coach_system.md`
- Modify: `internal/ai/prompts/prompts_test.go`
- Test: `internal/ai/prompts/prompts_test.go`

**Interfaces:**
- Consumes: `prompts.Raw(prompts.CoachSystem)` / `prompts.CoachSystem == "coach_system.md"`
- Produces: an 8th grounding rule whose phrases the existing `TestCoachPromptStatesGroundingRules` table will lock

- [ ] **Step 1: Extend the prompt-policy test so it fails**

In `internal/ai/prompts/prompts_test.go`, inside `TestCoachPromptStatesGroundingRules`, add two entries to the `required` map:

```go
		"treats approved memories as known facts": "treat \"known about them\" as facts",
		"admits a missing memory is unknown":      "if a fact you need is not listed there",
```

Keep the five existing entries. Do not reword them.

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./internal/ai/prompts/ -count=1 -run TestCoachPromptStatesGroundingRules
```

Expected: FAIL, looking for `treat "known about them" as facts`.

- [ ] **Step 3: Add grounding rule 8 to `coach_system.md`**

Insert this as a new numbered rule after rule 7 (the citation rule) and before the paragraph that begins "A confident wrong answer":

```markdown
8. **Treat "Known about them" as facts.** Those lines are approved profile
   facts this person (or you, after they confirmed) recorded. Use them as
   known. If a fact you need is not listed there, you do not know it — say
   so plainly and ask. Do not promote a check-in, a goal, or a guess into
   a durable fact, and do not soften a listed fact into a maybe.
```

Do not rewrite rules 1–7. Rule 1 still forbids inventing; rule 8 names the memory heading so the model can attach that rule to the right section.

- [ ] **Step 4: Re-run the prompt tests**

```bash
go test ./internal/ai/prompts/ -count=1
```

Expected: PASS, including `TestCoachPromptStatesGroundingRules` and `TestAllPromptsParse`.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/prompts/coach_system.md internal/ai/prompts/prompts_test.go
git commit -m "$(cat <<'EOF'
feat: treat approved memories as known coach facts

Name the Known about them section in the system prompt so the model
uses listed facts and admits anything that is not there.
EOF
)"
```

---

### Task 4: Builder and system-prompt wiring

**Files:**
- Modify: `internal/coach/context_builder_test.go`
- Modify: `internal/coach/service_test.go`
- Test: those two files

**Interfaces:**
- Consumes: `coach.NewContextBuilder`, `coach.Context.Render`, `coach.Evidence`, `PromptBuilder.Coach` via `SendMessage` (existing harness in `service_test.go`)
- Produces: two tests that fail until the assertions are true of current Render + the Task 3 prompt

- [ ] **Step 1: Add `TestMemoriesRenderUnderTheirHeading` next to `TestCheckInsRenderUnderTheirHeading`**

Append to `internal/coach/context_builder_test.go` (same package `coach_test`, reuse `testUser` / `fakeSource`):

```go
func TestMemoriesRenderUnderTheirHeading(t *testing.T) {
	pool := testdb.New(t)
	convos := conversations.NewService(conversations.NewRepository(pool))

	const ref = "memory:6f2c81a4-1111-2222-3333-444444444444"
	const text = "[injury, pinned] Left knee is sore on deep squats"

	filled, err := coach.NewContextBuilder(convos,
		fakeSource{name: "memories", fill: func(c *coach.Context) {
			c.Memories = append(c.Memories, coach.Evidence{
				Ref:   ref,
				Text:  text,
				Label: "profile fact",
			})
		}},
	).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	rendered := filled.Render()
	want := "Known about them:\n- [[" + ref + "]] " + text
	if !strings.Contains(rendered, want) {
		t.Fatalf("memories should render as cited bullets under their heading:\n%s", rendered)
	}

	empty, err := coach.NewContextBuilder(convos).Build(context.Background(), coach.ContextRequest{User: testUser()})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(empty.Render(), "Known about them: none recorded yet") {
		t.Fatalf("an empty memory section must say so rather than go missing:\n%s", empty.Render())
	}
}
```

- [ ] **Step 2: Extend `TestSystemPromptCarriesRulesAndContext`**

In `internal/coach/service_test.go`, add these strings to the `want` slice in `TestSystemPromptCarriesRulesAndContext`:

```go
		"Known about them: none recorded yet",
		`Treat "Known about them" as facts`,
```

The first is the empty-state label Render already emits. The second is the rule Task 3 added. Both must appear in the system prompt `SendMessage` actually sends.

- [ ] **Step 3: Run the coach tests**

```bash
go test ./internal/coach/ -count=1 -run 'TestMemoriesRenderUnderTheirHeading|TestSystemPromptCarriesRulesAndContext|TestAFailingSourceDoesNotAbortTheOthers|TestCheckInsRenderUnderTheirHeading'
```

Expected: PASS.

If `TestSystemPromptCarriesRulesAndContext` fails on the new rule phrase, the prompt file from Task 3 did not land — do not weaken the assertion.

- [ ] **Step 4: Commit**

```bash
git add internal/coach/context_builder_test.go internal/coach/service_test.go
git commit -m "$(cat <<'EOF'
test: assert memory facts reach the coach system prompt

Lock the Known about them heading, its empty label, and the known-facts
rule on the path SendMessage actually builds.
EOF
)"
```

---

### Task 5: Verification

**Files:** none new

- [ ] **Step 1: Run the packages this plan touched**

```bash
go test ./internal/memories/ ./internal/ai/prompts/ ./internal/coach/ ./internal/onboarding/ -count=1
```

Expected: all PASS. `internal/onboarding` calls `ForContext` to assert seeded memories; Collect's budget must not change that API.

- [ ] **Step 2: Confirm wiring is still in both binaries**

```bash
rg -n 'memories.NewContextSource' cmd/web/main.go cmd/mcp-server/main.go
```

Expected: one hit in each file.

- [ ] **Step 3: Confirm no prompt text leaked outside `internal/ai/prompts`**

```bash
rg -n 'Treat "Known about them" as facts' --glob '!internal/ai/prompts/**' --glob '!*_test.go'
```

Expected: no matches.

- [ ] **Step 4: Spec coverage check (do not skip)**

| Acceptance | Where it is proven |
| --- | --- |
| Memories appear in context when present | `TestContextSourceFillsMemories`, `TestMemoriesRenderUnderTheirHeading` |
| Empty memory is silent/graceful | Collect leaves the slice empty (`TestContextSourceStaysEmptyWhenNone`); Render labels it; Build does not error |
| Source failure does not fail coach reply | Source reports the error (`TestContextSourceSurfacesErrors`); builder continues (`TestAFailingSourceDoesNotAbortTheOthers`) |
| Pinned + recent/ranked approved | Existing `ForContext` + `TestContextSourcePrefersPinnedWithinBudget` + `TestSearchAlwaysKeepsPinnedFacts` |
| Short bullets, hard cap | `evidenceSection` bullets + `TestContextSourceHonoursCharBudget` |
| Treat memory as known facts, admit gaps | `coach_system.md` rule 8 + `TestCoachPromptStatesGroundingRules` + `TestSystemPromptCarriesRulesAndContext` |
| Follow goals ContextSource pattern | Same `Name`/`Collect`/`var _` shape; still registered next to `goals.NewContextSource` |

- [ ] **Step 5: Do not run live evals unless asked**

`go test -tags live ./internal/ai/eval/` costs provider money. The offline tests above are the gate. The existing live tests already cover "admit what you were not told" and "use facts you were given".

---

## Least confident decisions

1. **Empty is labelled, not omitted.** Linear says "silent/graceful". `DOMAIN.md` says empty sections are labelled so the model does not invent. The plan follows `DOMAIN.md`. Collect stays silent; Render says `none recorded yet`.
2. **2000 runes of `Summary()` text, not of the rendered line.** The `[[memory:uuid]]` wrapper is ~50 characters per fact and is not counted. If the section still feels large in a log, drop the constant, not the clip logic.
3. **Skip-and-continue rather than stop-at-first-overflow.** A short fact after a long one that does not fit still earns its seat. Pinned facts arrive first, so they are not the ones skipped.
4. **Budget lives in Collect, not `ForContext`.** Onboarding and search tests call `ForContext` as a retrieval API. Clipping there would hide facts from callers that are not building a prompt.
5. **Search ranking stays.** The Linear issue said "pinned + recent". The repo already does better (pinned + ranked, recent only with no query). Do not revert that.

## Out of scope

- Memory extraction job, pending-approval UI, pin/exclude controls
- Changing `contextLimit = 20` (row fetch bound stays; char budget is the new cap)
- Token-counting a real tokenizer — rune count is the budget the ticket asked for
- Live model evals
- Knowledge / goals / check-ins prompt wording
