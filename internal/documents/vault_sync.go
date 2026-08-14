package documents

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// RetryEmbeddings requeues failed embed jobs and schedules a fresh pass.
func (s *Service) RetryEmbeddings(ctx context.Context, userID uuid.UUID) error {
	if s.queue == nil {
		return fmt.Errorf("documents: no queue configured")
	}
	if _, err := s.queue.RequeueFailedEmbedJobsForUser(ctx, userID); err != nil {
		return err
	}
	_, err := s.queue.Enqueue(ctx, jobs.KindEmbedChunks, jobs.EmbedChunksPayload{UserID: userID})
	return err
}

// QueueIndex schedules one document for parsing and chunking.
func (s *Service) QueueIndex(ctx context.Context, userID, documentID uuid.UUID) error {
	if s.queue == nil {
		return fmt.Errorf("documents: no queue configured")
	}
	_, err := s.queue.Enqueue(ctx, jobs.KindIndexDocument, jobs.IndexDocumentPayload{
		UserID:     userID,
		DocumentID: documentID,
	})
	return err
}

// UpsertVaultFile stores or updates a vault-sourced document's bytes.
//
// Returns the document and whether its content changed enough to reindex.
func (s *Service) UpsertVaultFile(
	ctx context.Context,
	userID uuid.UUID,
	externalPath, title, mime string,
	content []byte,
	mtime time.Time,
) (Document, bool, error) {
	externalPath = filepath.ToSlash(strings.TrimPrefix(filepath.Clean(externalPath), "./"))
	if externalPath == "" || strings.HasPrefix(externalPath, "..") {
		return Document{}, false, apperr.FieldErrors{}.Add("path", "That path is not inside your vault.").OrNil()
	}
	if s.storage == nil {
		return Document{}, false, fmt.Errorf("documents: no object storage configured")
	}

	existing, err := s.repo.GetByExternalPath(ctx, userID, externalPath)
	if err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return Document{}, false, err
	}

	if apperr.Is(err, apperr.ErrNotFound) {
		key := fmt.Sprintf("vault/%s/%s", userID, uuid.NewString())
		if err := s.storage.Put(ctx, key, mime, bytes.NewReader(content)); err != nil {
			return Document{}, false, apperr.Wrap(err, "store vault file")
		}
		doc, err := s.repo.CreateVault(ctx, userID, title, key, mime, int64(len(content)), externalPath, mtime)
		if err != nil {
			return Document{}, false, err
		}
		return doc, true, nil
	}

	if existing.ExternalMtime != nil && !mtime.After(*existing.ExternalMtime) {
		return existing, false, nil
	}

	if err := s.storage.Put(ctx, existing.StorageKey, mime, bytes.NewReader(content)); err != nil {
		return Document{}, false, apperr.Wrap(err, "store vault file")
	}
	if err := s.repo.UpdateVault(ctx, existing.ID, title, mime, int64(len(content)), mtime); err != nil {
		return Document{}, false, err
	}
	existing.Title = title
	existing.MIME = mime
	existing.ByteSize = int64(len(content))
	existing.ExternalMtime = &mtime
	existing.Status = StatusPending
	return existing, true, nil
}

// ListVaultPaths returns vault-relative paths currently indexed for a user.
func (s *Service) ListVaultPaths(ctx context.Context, userID uuid.UUID) (map[string]uuid.UUID, error) {
	return s.repo.ListVaultPaths(ctx, userID)
}
