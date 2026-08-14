package vault_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/vault"
	vaultdb "github.com/NorthAIProject/north-client/internal/vault/db"
)

type memStorage struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func newMemStorage() *memStorage {
	return &memStorage{objs: map[string][]byte{}}
}

func (m *memStorage) Put(_ context.Context, key, _ string, body io.Reader) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = append([]byte(nil), b...)
	return nil
}

func (m *memStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, fmt.Errorf("missing %s", key)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func seedUser(t *testing.T, pool *pgxpool.Pool) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "vault-" + t.Name() + "@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

type vaultFixture struct {
	vault   *vault.Service
	docs    *documents.Service
	indexer *documents.Indexer
	queue   *jobs.Queue
	user    users.User
	ctx     context.Context
}

func newVaultFixture(t *testing.T) vaultFixture {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool)
	store := newMemStorage()
	queue := jobs.NewQueue(pool)
	repo := documents.NewRepository(pool)
	docSvc := documents.NewService(repo, store, queue)
	return vaultFixture{
		vault: vault.NewService(vault.Options{
			Repository: vault.NewRepository(vaultdb.New(pool)),
			Documents:  docSvc,
			Queue:      queue,
		}),
		docs:    docSvc,
		indexer: documents.NewIndexer(repo, store),
		queue:   queue,
		user:    user,
		ctx:     ctx,
	}
}

func TestVaultSyncIndexesMarkdown(t *testing.T) {
	f := newVaultFixture(t)

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "training.md"), []byte("# Training\n\nNarrow grip overhead press is fine.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := f.vault.Connect(f.ctx, f.user.ID, root); err != nil {
		t.Fatal(err)
	}
	if err := f.vault.Sync(f.ctx, f.user.ID); err != nil {
		t.Fatal(err)
	}

	paths, err := f.docs.ListVaultPaths(f.ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v", paths)
	}

	indexJob, ok, err := f.queue.Claim(f.ctx)
	if err != nil || !ok {
		t.Fatal("expected index job after vault sync")
	}
	if indexJob.Kind != jobs.KindIndexDocument {
		t.Fatalf("kind = %q", indexJob.Kind)
	}

	if err = f.indexer.HandleIndexDocument(f.ctx, indexJob.Payload); err != nil {
		t.Fatal(err)
	}

	hits, err := f.docs.Search(f.ctx, f.user.ID, "narrow grip overhead", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("vault note was not searchable after index")
	}
}

func TestVaultSyncRemovesDeletedFiles(t *testing.T) {
	f := newVaultFixture(t)

	root := t.TempDir()
	notePath := filepath.Join(root, "gone.md")
	if err := os.WriteFile(notePath, []byte("temporary note\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := f.vault.Connect(f.ctx, f.user.ID, root); err != nil {
		t.Fatal(err)
	}
	if err := f.vault.Sync(f.ctx, f.user.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(notePath); err != nil {
		t.Fatal(err)
	}
	if err := f.vault.Sync(f.ctx, f.user.ID); err != nil {
		t.Fatal(err)
	}

	paths, err := f.docs.ListVaultPaths(f.ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("deleted vault file still indexed: %v", paths)
	}
}

func TestVaultIgnoresObsidianFolder(t *testing.T) {
	f := newVaultFixture(t)

	root := t.TempDir()
	obsidian := filepath.Join(root, ".obsidian", "workspace.json")
	if err := os.MkdirAll(filepath.Dir(obsidian), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(obsidian, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "real.md"), []byte("# Real\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := f.vault.Connect(f.ctx, f.user.ID, root); err != nil {
		t.Fatal(err)
	}
	if err := f.vault.Sync(f.ctx, f.user.ID); err != nil {
		t.Fatal(err)
	}

	paths, err := f.docs.ListVaultPaths(f.ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for path := range paths {
		if strings.Contains(path, ".obsidian") {
			t.Fatalf("indexed obsidian path: %s", path)
		}
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v", paths)
	}
}

func TestHandleSyncJobRunsSync(t *testing.T) {
	f := newVaultFixture(t)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.vault.Connect(f.ctx, f.user.ID, root); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"user_id":"` + f.user.ID.String() + `"}`)
	if err := f.vault.HandleSyncJob(f.ctx, payload); err != nil {
		t.Fatal(err)
	}

	paths, err := f.docs.ListVaultPaths(f.ctx, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v", paths)
	}
}
