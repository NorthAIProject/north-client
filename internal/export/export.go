// Package export writes everything North knows about a person as a folder of
// plain text they can take anywhere.
//
// Its own package rather than a method on any one slice, because it is the only
// thing in North that legitimately reads across all of them — memories,
// documents, and conversations — and putting it inside one would make that
// slice import its peers for a reason unrelated to what it does.
package export

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/memories"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// conversationLimit bounds how many conversations one export covers. High
// enough that no real account is truncated.
const conversationLimit = 5000

// Exporter writes everything North knows about a person as a zip of Markdown
// and their own original files.
//
// This is the answer to the question worth asking of any system that keeps your
// knowledge: can you delete it tomorrow and still have what you put in. Nothing
// here is a proprietary format — the notes come out as the Markdown they went
// in as, and the derived parts (chunks, the search index) are deliberately
// absent, because they are rebuildable and are not yours in any meaningful way.
type Exporter struct {
	documents     *documents.Service
	memories      *memories.Service
	conversations *conversations.Service
	storage       documents.Storage
}

func NewExporter(
	docs *documents.Service,
	facts *memories.Service,
	convos *conversations.Service,
	storage documents.Storage,
) *Exporter {
	return &Exporter{documents: docs, memories: facts, conversations: convos, storage: storage}
}

// WriteZip streams the archive.
//
// Streamed rather than assembled: a person with years of history should not
// wait for the whole thing to be built in memory first, and the server should
// not hold it there.
//
// A failure part-way cannot be reported as a status code — the header is long
// gone — so problems with one section are written into the archive as a note
// rather than abandoning the rest. Half an export plus an explanation beats a
// truncated file with no clue why.
func (e *Exporter) WriteZip(ctx context.Context, user users.User, w io.Writer) error {
	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	if err := e.writeManifest(zw, user); err != nil {
		return err
	}
	if err := e.writeReadme(zw, user); err != nil {
		return err
	}

	var problems []string

	if err := e.writeMemories(ctx, zw, user); err != nil {
		problems = append(problems, "profile facts: "+err.Error())
	}
	if err := e.writeDocuments(ctx, zw, user); err != nil {
		problems = append(problems, "documents: "+err.Error())
	}
	if err := e.writeConversations(ctx, zw, user); err != nil {
		problems = append(problems, "conversations: "+err.Error())
	}

	if len(problems) > 0 {
		if err := writeFile(zw, "INCOMPLETE.txt", strings.Join(append(
			[]string{"Some parts of this export could not be written:", ""},
			problems...), "\n")+"\n"); err != nil {
			return err
		}
	}

	return zw.Close()
}

func (e *Exporter) writeManifest(zw *zip.Writer, user users.User) error {
	manifest := map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"account":     user.Email,
		"contents": []string{
			"memories.md — the facts North was told it may use",
			"documents/ — your notes and uploads, unchanged",
			"conversations/ — one Markdown file per conversation",
		},
		"not_included": []string{
			"the search index and the passages derived from your documents; " +
				"both are rebuilt from the files here and are not yours to keep",
		},
	}

	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return apperr.Wrap(err, "encode export manifest")
	}
	return writeFile(zw, "manifest.json", string(body)+"\n")
}

func (e *Exporter) writeReadme(zw *zip.Writer, user users.User) error {
	return writeFile(zw, "README.md", fmt.Sprintf(`# What North knows about %s

Everything in this archive is plain text. It opens in any editor, and it stays
readable whether or not North exists.

- `+"`memories.md`"+` — the durable facts North was told it may use in coaching.
- `+"`documents/`"+` — your notes and uploaded files, exactly as you gave them.
- `+"`conversations/`"+` — one file per conversation, oldest message first.

The search index is not here on purpose. It is built from the files above and
can be rebuilt from them, so it is North's working state rather than anything
of yours.

Exported %s.
`, user.DisplayName, time.Now().UTC().Format("2 January 2006")))
}

func (e *Exporter) writeMemories(ctx context.Context, zw *zip.Writer, user users.User) error {
	if e.memories == nil {
		return nil
	}

	facts, err := e.memories.ListApproved(ctx, user.ID)
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# What North remembers\n\n")
	if len(facts) == 0 {
		b.WriteString("Nothing recorded yet.\n")
		return writeFile(zw, "memories.md", b.String())
	}

	for _, f := range facts {
		fmt.Fprintf(&b, "- **%s**", f.Content)
		if f.Category != "" {
			fmt.Fprintf(&b, " _(%s", f.Category)
			if f.Pinned {
				b.WriteString(", pinned")
			}
			b.WriteString(")_")
		}
		if !f.CreatedAt.IsZero() {
			fmt.Fprintf(&b, " — added %s", f.CreatedAt.Format("2 Jan 2006"))
		}
		b.WriteString("\n")
	}

	return writeFile(zw, "memories.md", b.String())
}

func (e *Exporter) writeDocuments(ctx context.Context, zw *zip.Writer, user users.User) error {
	docs, err := e.documents.List(ctx, user.ID)
	if err != nil {
		return err
	}

	used := make(map[string]int, len(docs))
	for _, doc := range docs {
		name := path.Join("documents", uniqueName(used, doc))

		if doc.SourceKind == documents.SourceNote {
			if err := writeFile(zw, name, doc.Body); err != nil {
				return err
			}
			continue
		}
		if e.storage == nil || doc.StorageKey == "" {
			continue
		}

		body, err := e.storage.Get(ctx, doc.StorageKey)
		if err != nil {
			// One unreadable blob must not cost the person the rest of their
			// export; the note in its place says what happened.
			if err := writeFile(zw, name+".missing.txt",
				"North could not read this file back from storage.\n"); err != nil {
				return err
			}
			continue
		}

		entry, err := zw.Create(name)
		if err != nil {
			_ = body.Close()
			return apperr.Wrap(err, "start export entry")
		}
		_, copyErr := io.Copy(entry, body)
		_ = body.Close()
		if copyErr != nil {
			return apperr.Wrap(copyErr, "write export entry")
		}
	}
	return nil
}

func (e *Exporter) writeConversations(ctx context.Context, zw *zip.Writer, user users.User) error {
	if e.conversations == nil {
		return nil
	}

	convos, err := e.conversations.List(ctx, user.ID, conversationLimit)
	if err != nil {
		return err
	}

	used := make(map[string]int, len(convos))
	for _, c := range convos {
		messages, err := e.conversations.History(ctx, c.ID)
		if err != nil {
			return err
		}

		var b strings.Builder
		title := strings.TrimSpace(c.Title)
		if title == "" {
			title = "Untitled conversation"
		}
		fmt.Fprintf(&b, "# %s\n\n_%s_\n\n", title, c.CreatedAt.Format("2 January 2006"))

		for _, m := range messages {
			who := "North"
			if m.IsUser() {
				who = user.DisplayName
			}
			fmt.Fprintf(&b, "**%s** — %s\n\n%s\n\n",
				who, m.CreatedAt.Format("15:04"), strings.TrimSpace(m.Content))

			// The refs are what the coach was working from. Kept, because a
			// reply nobody can check is the thing this whole export exists to
			// argue against.
			if len(m.EvidenceRefs) > 0 {
				fmt.Fprintf(&b, "_Drew on: %s_\n\n", strings.Join(m.EvidenceRefs, ", "))
			}
		}

		name := slug(c.CreatedAt.Format("2006-01-02") + "-" + title)
		fmt.Fprintf(&b, "\n")
		if err := writeFile(zw, path.Join("conversations", nextName(used, name, ".md")), b.String()); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(zw *zip.Writer, name, body string) error {
	entry, err := zw.Create(name)
	if err != nil {
		return apperr.Wrap(err, "start export entry %s", name)
	}
	if _, err := io.WriteString(entry, body); err != nil {
		return apperr.Wrap(err, "write export entry %s", name)
	}
	return nil
}

// uniqueName gives a document a filename a person would recognise, keeping the
// original extension for an upload.
func uniqueName(used map[string]int, doc documents.Document) string {
	ext := ".md"
	if doc.SourceKind == documents.SourceUpload {
		if e := path.Ext(doc.StorageKey); e != "" {
			ext = e
		}
	}
	return nextName(used, slug(doc.Title), ext)
}

// nextName disambiguates two documents that would otherwise collide.
//
// Two notes called "Physio notes" are entirely normal, and silently letting the
// second overwrite the first would lose data in an export whose entire purpose
// is not losing data.
func nextName(used map[string]int, base, ext string) string {
	if base == "" {
		base = "untitled"
	}
	used[base]++
	if n := used[base]; n > 1 {
		return fmt.Sprintf("%s-%d%s", base, n, ext)
	}
	return base + ext
}

func slug(s string) string {
	var b strings.Builder
	lastDash := true

	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
