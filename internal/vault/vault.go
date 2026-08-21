package vault

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Documents is the vault sync view of the knowledge service.
type Documents interface {
	UpsertVaultFile(ctx context.Context, userID uuid.UUID, externalPath, title, mime string, content []byte, mtime time.Time) (documents.Document, bool, error)
	ListVaultPaths(ctx context.Context, userID uuid.UUID) (map[string]uuid.UUID, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
	QueueIndex(ctx context.Context, userID, documentID uuid.UUID) error
}

// Enqueuer schedules background work.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind jobs.Kind, payload any) (jobs.Job, error)
}

type Service struct {
	repo  *Repository
	docs  Documents
	queue Enqueuer
}

type Options struct {
	Repository *Repository
	Documents  Documents
	Queue      Enqueuer
}

func NewService(opts Options) *Service {
	return &Service{repo: opts.Repository, docs: opts.Documents, queue: opts.Queue}
}

// Connect saves the vault root path for a user.
func (s *Service) Connect(ctx context.Context, userID uuid.UUID, rootPath string) (Connection, error) {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	if rootPath == "" {
		return Connection{}, apperr.FieldErrors{}.Add("path", "Enter the full path to your vault folder.").OrNil()
	}
	info, err := os.Stat(rootPath)
	if err != nil || !info.IsDir() {
		return Connection{}, apperr.FieldErrors{}.Add("path", "Khepri could not find that folder on this machine.").OrNil()
	}

	conn, err := s.repo.Upsert(ctx, userID, rootPath)
	if err != nil {
		return Connection{}, err
	}

	if s.queue != nil {
		_, _ = s.queue.Enqueue(ctx, jobs.KindSyncVault, jobs.SyncVaultPayload{UserID: userID})
	}
	return conn, nil
}

func (s *Service) Disconnect(ctx context.Context, userID uuid.UUID) error {
	return s.repo.Delete(ctx, userID)
}

func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Connection, error) {
	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return Connection{}, nil
		}
		return Connection{}, err
	}
	return conn, nil
}

// SyncNow queues a vault sync for one user.
func (s *Service) SyncNow(ctx context.Context, userID uuid.UUID) error {
	if s.queue == nil {
		return apperr.New("vault sync is not configured")
	}
	_, err := s.queue.Enqueue(ctx, jobs.KindSyncVault, jobs.SyncVaultPayload{UserID: userID})
	return err
}

// Sync walks the vault and upserts documents.
func (s *Service) Sync(ctx context.Context, userID uuid.UUID) error {
	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		return err
	}
	if !conn.Enabled {
		return nil
	}

	known, err := s.docs.ListVaultPaths(ctx, userID)
	if err != nil {
		_ = s.repo.MarkFailed(ctx, userID, err.Error())
		return err
	}

	seen := make(map[string]bool)
	walkErr := filepath.WalkDir(conn.RootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if shouldIgnoreDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldIgnoreFile(d.Name()) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !acceptedVaultExt[ext] {
			return nil
		}

		rel, err := filepath.Rel(conn.RootPath, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true

		info, err := d.Info()
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		title := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		mime := mimeForExt(ext)
		doc, changed, err := s.docs.UpsertVaultFile(ctx, userID, rel, title, mime, body, info.ModTime())
		if err != nil {
			return err
		}
		if changed {
			if err := s.docs.QueueIndex(ctx, userID, doc.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if walkErr != nil {
		_ = s.repo.MarkFailed(ctx, userID, walkErr.Error())
		return walkErr
	}

	for path, id := range known {
		if !seen[path] {
			if err := s.docs.Delete(ctx, id, userID); err != nil {
				return err
			}
		}
	}

	return s.repo.MarkSynced(ctx, userID)
}

var acceptedVaultExt = map[string]bool{
	".md": true, ".markdown": true, ".mdown": true,
	".txt": true, ".text": true, ".log": true, ".csv": true,
	".pdf": true,
}

func mimeForExt(ext string) string {
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".txt", ".text", ".log", ".csv":
		return "text/plain"
	default:
		return "text/markdown"
	}
}

func shouldIgnoreDir(name string) bool {
	switch name {
	case ".obsidian", ".git", "node_modules", ".trash", ".cursor":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}

func shouldIgnoreFile(name string) bool {
	return strings.HasPrefix(name, ".")
}
