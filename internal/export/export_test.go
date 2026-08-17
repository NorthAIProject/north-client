package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/export"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Sam",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

const note = `# Physio notes

Wide-grip overhead pressing aggravates the left shoulder.
`

// The claim this whole feature makes is that you can delete North tomorrow and
// still have what you put in. That is worth checking rather than asserting.
func TestExportCarriesEverythingAPersonPutIn(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "export@north.test")

	docRepo := documents.NewRepository(pool)
	docSvc := documents.NewService(docRepo, nil, nil)
	memorySvc := memories.NewService(memories.NewRepository(pool))
	convoSvc := conversations.NewService(conversations.NewRepository(pool))
	goalSvc := goals.NewService(goals.NewRepository(pool))
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)

	if _, err := docSvc.CreateNote(ctx, user.ID, "Physio notes", note); err != nil {
		t.Fatal(err)
	}
	if _, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title:      "Press bodyweight overhead",
		Category:   goals.CategoryFitness,
		Motivation: "The shoulder should stop deciding what I can lift.",
		Success:    "One clean rep at bodyweight.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := checkinSvc.UpsertToday(ctx, user, checkins.Input{
		Mood: 4, Energy: 3,
		Wins:       "Pressed narrow grip with no pain",
		Challenges: "Sleep was short",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := memorySvc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryInjury,
		Content:  "Left shoulder dislocates on wide-grip pressing",
	}); err != nil {
		t.Fatal(err)
	}

	convo, err := convoSvc.Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := convoSvc.SetTitle(ctx, convo.ID, "Shoulder plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := convoSvc.AppendUserMessage(ctx, convo.ID, "Can I press overhead this week?", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := convoSvc.AppendModelMessage(ctx, convo.ID, "Narrow grip only.", nil, "m", "p",
		[]string{"memory:6f2c81a4-0000-4000-8000-000000000001"}); err != nil {
		t.Fatal(err)
	}

	files := unzip(t, export.NewExporter(export.Options{
		Documents:     docSvc,
		Memories:      memorySvc,
		Conversations: convoSvc,
		Goals:         goalSvc,
		CheckIns:      checkinSvc,
	}), user)

	for _, name := range []string{"README.md", "manifest.json", "memories.md", "profile.md", "goals.md", "check-ins.md"} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s is missing from the export", name)
		}
	}
	if _, ok := files["INCOMPLETE.txt"]; ok {
		t.Errorf("the export reported problems: %s", files["INCOMPLETE.txt"])
	}

	if got := files["memories.md"]; !strings.Contains(got, "Left shoulder dislocates") {
		t.Errorf("memories.md does not contain the fact:\n%s", got)
	}

	// The account record itself, the goals, and the days logged against them.
	// Each was in North and absent from the archive until this ticket, which is
	// the gap worth a test rather than the presence of a file.
	for _, tc := range []struct{ file, want string }{
		{"profile.md", "export@north.test"},
		{"profile.md", "Sam"},
		{"goals.md", "Press bodyweight overhead"},
		{"goals.md", "The shoulder should stop deciding"},
		{"check-ins.md", "Pressed narrow grip with no pain"},
		{"check-ins.md", "Sleep was short"},
	} {
		if got := files[tc.file]; !strings.Contains(got, tc.want) {
			t.Errorf("%s does not contain %q:\n%s", tc.file, tc.want, got)
		}
	}

	docFile := findFile(files, "documents/")
	if docFile == "" {
		t.Fatalf("no document was exported; got %v", names(files))
	}
	// The note must come out exactly as it is stored. CreateNote trims
	// surrounding whitespace on the way in, so that — and only that — is the
	// difference from the string above; the body itself is untouched.
	if want := strings.TrimSpace(note); files[docFile] != want {
		t.Errorf("the note was altered by the export:\n got: %q\nwant: %q", files[docFile], want)
	}

	convoFile := findFile(files, "conversations/")
	if convoFile == "" {
		t.Fatalf("no conversation was exported; got %v", names(files))
	}
	body := files[convoFile]
	for _, want := range []string{"Shoulder plan", "Can I press overhead", "Narrow grip only", "Sam"} {
		if !strings.Contains(body, want) {
			t.Errorf("the conversation export is missing %q:\n%s", want, body)
		}
	}
	// The refs are what the reply was built from; an export that drops them
	// hands over the answers without the evidence.
	if !strings.Contains(body, "memory:6f2c81a4") {
		t.Errorf("the conversation export dropped the evidence refs:\n%s", body)
	}
}

// Two documents with one name is ordinary. Losing one of them in an export
// whose entire purpose is not losing things would not be.
func TestExportDoesNotLoseDocumentsWithTheSameName(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "export-dupes@north.test")

	docSvc := documents.NewService(documents.NewRepository(pool), nil, nil)
	for _, body := range []string{"First version of the notes.", "Second version of the notes."} {
		if _, err := docSvc.CreateNote(ctx, user.ID, "Physio notes", body); err != nil {
			t.Fatal(err)
		}
	}

	files := unzip(t, export.NewExporter(export.Options{
		Documents:     docSvc,
		Memories:      memories.NewService(memories.NewRepository(pool)),
		Conversations: conversations.NewService(conversations.NewRepository(pool)),
	}), user)

	var found int
	for name := range files {
		if strings.HasPrefix(name, "documents/") {
			found++
		}
	}
	if found != 2 {
		t.Errorf("exported %d of 2 documents: %v", found, names(files))
	}
}

func TestExportIsReadableWithoutNorth(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "export-plain@north.test")

	docSvc := documents.NewService(documents.NewRepository(pool), nil, nil)
	if _, err := docSvc.CreateNote(ctx, user.ID, "Physio notes", note); err != nil {
		t.Fatal(err)
	}

	files := unzip(t, export.NewExporter(export.Options{
		Documents:     docSvc,
		Memories:      memories.NewService(memories.NewRepository(pool)),
		Conversations: conversations.NewService(conversations.NewRepository(pool)),
	}), user)

	// Nothing in the archive should be a format that needs North to read it.
	for name, body := range files {
		if strings.HasSuffix(name, ".json") {
			continue
		}
		if !isPlainText(body) {
			t.Errorf("%s is not plain text", name)
		}
	}

	// And the derived state must not be in there: it is North's working
	// state, rebuildable from the files above, and not the person's.
	for name := range files {
		if strings.Contains(name, "chunk") || strings.Contains(name, "index") {
			t.Errorf("the export includes derived state: %s", name)
		}
	}
}

func unzip(t *testing.T, e *export.Exporter, user users.User) map[string]string {
	t.Helper()

	var buf bytes.Buffer
	if err := e.WriteZip(context.Background(), user, &buf); err != nil {
		t.Fatal(err)
	}

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("the export is not a readable zip: %v", err)
	}

	out := make(map[string]string, len(r.File))
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(body)
	}
	return out
}

func findFile(files map[string]string, prefix string) string {
	for name := range files {
		if strings.HasPrefix(name, prefix) {
			return name
		}
	}
	return ""
}

func names(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}

func isPlainText(s string) bool {
	for _, r := range s {
		if r == 0 {
			return false
		}
	}
	return true
}
