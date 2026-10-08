package planimport

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/users"
)

func readerWith(client *fake.Client) *AIReader {
	registry := ai.NewRegistry()
	registry.Register(client)
	return NewAIReader(ai.NewRunner(registry, ai.NewChainSet([]string{client.Name()}, nil)), "")
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
	name, rows, unparsed, err := reader.ReadWorkout(context.Background(), users.User{}, src)
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

	_, rows, _, err := reader.ReadMeal(context.Background(), users.User{}, Source{Kind: KindImage, Image: pngPixel, MIME: "image/png"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 1 || rows[0].Food != "Rice" {
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
	_, _, _, err := readerWith(client).ReadWorkout(context.Background(), users.User{}, Source{Kind: KindText, Text: "banana bread"})
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
		_, err := svc.ParseWorkout(context.Background(), user, "plan.txt", []byte("Squat 3x5"))
		done <- err
	}()
	<-started

	if _, err := svc.ParseWorkout(context.Background(), user, "plan.txt", []byte("Squat 3x5")); ReasonOf(err) != ReasonBusy {
		t.Fatalf("second import err = %v, want busy", err)
	}
	close(release)
	<-done

	// Finished means free again.
	if _, err := svc.ParseWorkout(context.Background(), user, "plan.csv", []byte("exercise\nSquat")); err != nil {
		t.Fatalf("import after the first finished: %v", err)
	}
}
