package planimport

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/users"
)

func readerWith(client *fake.Client) *AIReader {
	registry := ai.NewRegistry()
	registry.Register(client)
	return NewAIReader(ai.NewRunner(registry, ai.NewChainSet([]string{client.Name()}, nil)), "", nil)
}

func TestAIReaderTranscribesThroughTheSameCellParsers(t *testing.T) {
	t.Parallel()

	client := fake.Text(`{"is_plan":"yes","not_plan_reason":"","name":"Push Pull",
	  "rows":[
	    {"day":"Day 1","exercise":"Bench Press","sets":"","reps":"3x8","load":"80 kg","rest":"2 min","notes":"pause on chest","confidence":"high"},
	    {"day":"Day 1","exercise":"Dips","sets":"","reps":"","load":"","rest":"","notes":"","confidence":"low"}
	  ],
	  "unparsed":["Superset the last two"]}`)
	reader := readerWith(client)

	src := Source{Kind: KindPDF, Filename: "plan.pdf", Text: "Day 1\nBench Press 3x8 80 kg"}
	name, rows, unparsed, err := reader.ReadWorkout(context.Background(), users.User{}, src, "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	draft, err := buildWorkout(name, rows, unparsed)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	bench := draft.Days[0].Exercises[0]
	if bench.Sets != nil {
		t.Fatalf("bench sets = %v: a 3x8 in the reps column is not a set count to trust without a sets column", *bench.Sets)
	}
	if *bench.RestSeconds != 120 || bench.Load != "80 kg" || bench.Notes != "pause on chest" {
		t.Fatalf("bench = %+v", bench)
	}
	dips := draft.Days[0].Exercises[1]
	if dips.Sets != nil || dips.Reps != "" || len(dips.Flags) != 1 {
		t.Fatalf("dips = %+v, want nothing filled in and a low-confidence flag", dips)
	}
	if len(draft.Unparsed) != 1 {
		t.Fatalf("unparsed = %q", draft.Unparsed)
	}

	call := client.LastCall()
	if !strings.Contains(call.System, "do not invent") {
		t.Fatalf("system prompt lost its rule against inventing values")
	}
	if strings.Contains(call.System, "Bench Press") {
		t.Fatalf("the file's text belongs in the user message, not the system prompt")
	}
	if got := call.Messages[0].Parts[0].Text; !strings.Contains(got, "<source>") || !strings.Contains(got, "Bench Press") {
		t.Fatalf("user message = %q", got)
	}
	if call.ResponseSchema == nil || call.Temperature == nil || *call.Temperature != 0 {
		t.Fatalf("request must be schema-constrained at temperature 0")
	}
}

func TestAIReaderSendsAPhotoAsInlineData(t *testing.T) {
	t.Parallel()

	client := fake.Text(`{"is_plan":"yes","not_plan_reason":"","name":"","rows":[{"day":"Mon","meal":"Lunch","food":"Rice","quantity":"200","unit":"g","protein":"","carbs":"","fat":"","confidence":"high"}],"unparsed":[]}`)
	reader := readerWith(client)

	reading, err := reader.ReadMeal(context.Background(), users.User{}, Source{Kind: KindImage, Image: pngPixel, MIME: "image/png"}, "")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if rows := reading.Rows; len(rows) != 1 || rows[0].Food != "Rice" {
		t.Fatalf("rows = %+v", rows)
	}
	part := client.LastCall().Messages[0].Parts[0]
	if part.MIMEType != "image/png" || len(part.InlineData) == 0 {
		t.Fatalf("part = %+v, want the image inline", part)
	}
}

func TestAIReaderRefusesWhatIsNotAPlan(t *testing.T) {
	t.Parallel()

	client := fake.Text(`{"is_plan":"no","not_plan_reason":"it is a recipe for banana bread.","name":"","rows":[],"unparsed":[]}`)
	_, _, _, err := readerWith(client).ReadWorkout(context.Background(), users.User{}, Source{Kind: KindText, Text: "banana bread"}, "")
	if ReasonOf(err) != ReasonNotAPlan {
		t.Fatalf("err = %v, want not a plan", err)
	}
	if !strings.Contains(err.Error(), "banana bread") {
		t.Fatalf("message %q should say what the file is instead", err)
	}
}

func TestOneImportAtATime(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{})
	client := &fake.Client{Handler: func(ctx context.Context, _ ai.Request) (fake.Response, error) {
		close(started)
		<-release
		return fake.Response{Text: `{"is_plan":"no","not_plan_reason":"","name":"","rows":[],"unparsed":[]}`}, nil
	}}
	svc := NewService(Options{Reader: readerWith(client)})
	user := users.User{}

	done := make(chan error)
	go func() {
		_, err := svc.ParseWorkout(context.Background(), user, "plan.txt", []byte("Squat 3x5"), "")
		done <- err
	}()
	<-started

	if _, err := svc.ParseWorkout(context.Background(), user, "plan.txt", []byte("Squat 3x5"), ""); ReasonOf(err) != ReasonBusy {
		t.Fatalf("second import err = %v, want busy", err)
	}
	close(release)
	<-done

	// Finished means free again.
	if _, err := svc.ParseWorkout(context.Background(), user, "plan.csv", []byte("exercise\nSquat"), ""); err != nil {
		t.Fatalf("import after the first finished: %v", err)
	}
}

func TestAIReaderReadsOptionsStandInsNotesAndSameAs(t *testing.T) {
	t.Parallel()

	client := fake.Text(`{"is_plan":"yes","not_plan_reason":"","name":"Plano alimentar",
	  "rows":[
	    {"day":"","meal":"Pequeno-almoço","option":"Opção 1","food":"pão integral","food_en":"Wholemeal bread","quantity":"2","unit":"fatias","grams_estimate":"60","protein":"","carbs":"","fat":"","confidence":"high"},
	    {"day":"","meal":"Pequeno-almoço","option":"Opção 2","food":"iogurte","food_en":"Plain yogurt","quantity":"1","unit":"","grams_estimate":"125","protein":"","carbs":"","fat":"","confidence":"high"},
	    {"day":"","meal":"Almoço","option":"Prato – carne","food":"frango","food_en":"Chicken breast","quantity":"125","unit":"g","grams_estimate":"","protein":"","carbs":"","fat":"","confidence":"high"},
	    {"day":"","meal":"Almoço","option":"Prato – peixe","food":"pescada","food_en":"Hake","quantity":"150","unit":"g","grams_estimate":"","protein":"","carbs":"","fat":"","confidence":"high"}
	  ],
	  "unparsed":[],
	  "notes":"## Hidratação\nBeber 1,5 L de água.",
	  "same_as":[{"meal":"Jantar","same_as":"Almoço"}]}`)
	catalogCalls := 0
	reader := NewAIReader(ai.NewRunner(registryOf(client), ai.NewChainSet([]string{client.Name()}, nil)), "", func(context.Context) ([]string, error) {
		catalogCalls++
		return []string{"Wholemeal bread", "Hake fillet"}, nil
	})

	src := Source{Kind: KindPDF, Filename: "plano.pdf", Text: "Pequeno-almoço Opção 1: 2 fatias pão integral"}
	reading, err := reader.ReadMeal(context.Background(), users.User{}, src, "only the weekday meals")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	draft, err := buildMeal(reading)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if !draft.EveryDay || draft.Notes != "## Hidratação\nBeber 1,5 L de água." {
		t.Fatalf("everyDay = %v, notes = %q", draft.EveryDay, draft.Notes)
	}
	meals := draft.Days[0].Meals
	if len(meals) != 3 || meals[2].Name != "Jantar" || meals[2].OptionLabel != "Prato – carne" || len(meals[2].Alternatives) != 1 {
		t.Fatalf("meals = %+v, want Jantar copied from Almoço", meals)
	}
	bread := meals[0].Foods[0]
	if bread.Food != "Wholemeal bread" || bread.SourceText != "2 fatias pão integral" || !bread.Estimated || *bread.Grams != 60 {
		t.Fatalf("bread = %+v", bread)
	}
	if alt := meals[0].Alternatives; len(alt) != 1 || alt[0].Label != "Opção 2" || alt[0].Foods[0].Food != "Plain yogurt" {
		t.Fatalf("breakfast alternatives = %+v", alt)
	}

	call := client.LastCall()
	for _, rule := range []string{"`option`", "`food_en`", "`grams_estimate`", "`notes`", "`same_as`", "Wholemeal bread", "Hake fillet"} {
		if !strings.Contains(call.System, rule) {
			t.Fatalf("system prompt is missing %s", rule)
		}
	}
	if strings.Contains(call.System, "only the weekday meals") || strings.Contains(call.System, "Opção 1: 2 fatias pão integral") {
		t.Fatalf("the source and the request belong in the user message, not the system prompt")
	}
	user := ""
	for _, p := range call.Messages[0].Parts {
		user += p.Text
	}
	if !strings.Contains(user, "<source>") || !strings.Contains(user, "<request>\nonly the weekday meals\n</request>") ||
		strings.Index(user, "<request>") < strings.Index(user, "</source>") {
		t.Fatalf("user message = %q, want the request after the source", user)
	}

	// The catalog is read once, not on every import.
	if _, err := reader.ReadMeal(context.Background(), users.User{}, src, ""); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if catalogCalls != 1 {
		t.Fatalf("catalog read %d times, want once", catalogCalls)
	}
	if user := client.LastCall().Messages[0].Parts; len(user) != 1 {
		t.Fatalf("parts = %+v, want no request part without a hint", user)
	}
}

func TestAIReaderReadsWithoutACatalogWhenItCannotBeLoaded(t *testing.T) {
	t.Parallel()

	reply := `{"is_plan":"yes","not_plan_reason":"","name":"","rows":[{"day":"","meal":"Lunch","option":"","food":"rice","food_en":"White rice","quantity":"100","unit":"g","grams_estimate":"","protein":"","carbs":"","fat":"","confidence":"high"}],"unparsed":[],"notes":"","same_as":[]}`
	src := Source{Kind: KindText, Filename: "plan.txt", Text: "Lunch: 100 g rice"}

	without := fake.Text(reply)
	if _, err := readerWith(without).ReadMeal(context.Background(), users.User{}, src, ""); err != nil {
		t.Fatalf("read without a catalog: %v", err)
	}

	failing := fake.Text(reply)
	calls := 0
	reader := NewAIReader(ai.NewRunner(registryOf(failing), ai.NewChainSet([]string{failing.Name()}, nil)), "", func(context.Context) ([]string, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("database is down")
		}
		return []string{"Basmati rice"}, nil
	})
	if _, err := reader.ReadMeal(context.Background(), users.User{}, src, ""); err != nil {
		t.Fatalf("read with a failing catalog: %v", err)
	}
	if strings.Contains(failing.LastCall().System, "Basmati rice") || strings.Contains(failing.LastCall().System, "## Catalog") {
		t.Fatalf("a failed catalog read still rendered a catalog")
	}
	if strings.Contains(without.LastCall().System, "## Catalog") {
		t.Fatalf("no catalog provider still rendered a catalog section")
	}
	// A failure is not remembered: the next import tries again.
	if _, err := reader.ReadMeal(context.Background(), users.User{}, src, ""); err != nil {
		t.Fatalf("read after the catalog recovered: %v", err)
	}
	if !strings.Contains(failing.LastCall().System, "Basmati rice") {
		t.Fatalf("the catalog was not retried after a failure")
	}
}

func registryOf(client *fake.Client) *ai.Registry {
	registry := ai.NewRegistry()
	registry.Register(client)
	return registry
}

// ai.Object makes required exactly the fields it is given in order; a field
// left out would silently become optional and drop from Gemini's ordering.
func TestMealSchemaRequiresEveryField(t *testing.T) {
	t.Parallel()

	row := mealSchema.Properties["rows"].Items
	if got := strings.Join(row.Required, ","); got != "day,meal,option,food,food_en,quantity,unit,grams_estimate,protein,carbs,fat,confidence" {
		t.Fatalf("row required = %s", got)
	}
	if got := strings.Join(mealSchema.Required, ","); got != "is_plan,not_plan_reason,name,rows,unparsed,notes,same_as" {
		t.Fatalf("reply required = %s", got)
	}
	if got := strings.Join(mealSchema.Properties["same_as"].Items.Required, ","); got != "meal,same_as" {
		t.Fatalf("same_as required = %s", got)
	}
	if got := strings.Join(workoutSchema.Required, ","); got != "is_plan,not_plan_reason,name,rows,unparsed" {
		t.Fatalf("workout reply required = %s, want it unchanged", got)
	}
}

// A PDF without a text layer reaches the model as the PDF itself, and the
// reading goes on to a draft like any other.
func TestAPDFWithoutATextLayerIsReadAsADocument(t *testing.T) {
	t.Parallel()

	client := fake.Text(`{"is_plan":"yes","not_plan_reason":"","name":"Força",
	  "rows":[{"day":"Segunda","exercise":"Agachamento","sets":"3","reps":"5","load":"","rest":"","notes":"","confidence":"high"}],
	  "unparsed":[]}`)
	svc := NewService(Options{Reader: readerWith(client)})
	data := minimalPDF(t, []string{" "}, false)

	draft, err := svc.ParseWorkout(context.Background(), users.User{}, "plano.pdf", data, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(draft.Days) != 1 || len(draft.Days[0].Exercises) != 1 || draft.Days[0].Exercises[0].Name != "Agachamento" {
		t.Fatalf("draft = %+v", draft)
	}

	call := client.LastCall()
	part := call.Messages[0].Parts[0]
	if part.MIMEType != "application/pdf" || !bytes.Equal(part.InlineData, data) {
		t.Fatalf("part = %q with %d bytes, want the PDF inline", part.MIMEType, len(part.InlineData))
	}
	if !strings.Contains(call.System, "attached PDF") || strings.Contains(call.System, "<source> tags") {
		t.Fatalf("the prompt should say the source is an attached PDF, not extracted text:\n%s", call.System)
	}
}

func TestTheMealPromptReadsAPDFDocument(t *testing.T) {
	t.Parallel()

	client := fake.Text(`{"is_plan":"yes","not_plan_reason":"","name":"","rows":[],"unparsed":[],"notes":"","same_as":[]}`)
	src, err := Open("plano.pdf", minimalPDF(t, []string{" "}, false))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := readerWith(client).ReadMeal(context.Background(), users.User{}, src, ""); err != nil {
		t.Fatalf("read: %v", err)
	}
	call := client.LastCall()
	if !strings.Contains(call.System, "attached PDF") || strings.Contains(call.System, "<source> tags") {
		t.Fatalf("the meal prompt should say the source is an attached PDF:\n%s", call.System)
	}
	if call.Messages[0].Parts[0].MIMEType != "application/pdf" {
		t.Fatalf("part = %+v, want the PDF inline", call.Messages[0].Parts[0])
	}
}

// When every provider fails on a PDF document, the person hears what they can
// do instead, as for a photo.
func TestAPDFDocumentNoProviderCouldReadSaysWhatToDo(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Handler: func(context.Context, ai.Request) (fake.Response, error) {
		return fake.Response{}, errors.New("provider: unsupported content type")
	}}
	src := Source{Kind: KindPDFDocument, Filename: "plano.pdf", Document: []byte("%PDF-1.4"), MIME: "application/pdf"}
	_, _, _, err := readerWith(client).ReadWorkout(context.Background(), users.User{}, src, "")
	if ReasonOf(err) != ReasonCannotRead || !strings.Contains(err.Error(), "PDF") {
		t.Fatalf("err = %v, want a cannot-read refusal about the PDF", err)
	}
}
