package messaging

import (
	"bytes"
	"context"
	"io"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/media"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Documents sent to the bot: a diet PDF, a training spreadsheet.
//
// A document is stored exactly as the app's chat stores an attached one, and
// the turn carries it as an attachment with the caption as its words. From
// there the coach does what it does with any attached file — reads it, and can
// import it as a plan — so nothing here knows what a diet is.

// Attachments stores a file the coach should read this turn. The media
// service's StoreChatAttachment, which the app's chat uploads go through.
type Attachments interface {
	StoreChatAttachment(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (conversations.Attachment, error)
}

// AcceptsFile reports whether a document is one the coach can be sent: a type
// the media service reads, no bigger than it stores.
//
// Judged on the name and size the platform reported, so an adapter can leave
// a refused file where it is rather than download it to throw away. Bytes
// already fetched count too, because the reported size is the sender's claim.
func AcceptsFile(f InboundFile) bool {
	size := max(f.SizeBytes, int64(len(f.Bytes)))
	return size <= media.MaxFileBytes && media.AcceptsFileName(f.Name)
}

// fileRefusal is what to say about a document the coach will not be sent, and
// whether there is one. Photos and voice notes are not its business.
func fileRefusal(ctx context.Context, f *InboundFile) (string, bool) {
	if f == nil || f.Kind != KindFile || AcceptsFile(*f) {
		return "", false
	}
	return i18n.Tf(ctx, "tg.file.refused", media.MaxFileBytes>>20), true
}

// incomingDocument stores a document and puts it on the turn, or says why it
// could not be.
func (s *Service) incomingDocument(ctx context.Context, userID uuid.UUID, out coach.Incoming, f InboundFile) (coach.Incoming, string, error) {
	if s.documents == nil {
		return coach.Incoming{}, i18n.T(ctx, "tg.file.unavailable"), nil
	}
	stored, err := s.documents.StoreChatAttachment(ctx, userID, f.Name, int64(len(f.Bytes)), bytes.NewReader(f.Bytes))
	if err != nil {
		// The media service's refusal names what was wrong with the file —
		// not a PDF after all, not UTF-8 text — so it is worth passing on.
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			if msg := fieldErrs.Messages()["attachment"]; msg != "" {
				return coach.Incoming{}, msg, nil
			}
		}
		s.log.Warn("messaging could not store a document", "error", err, "user_id", userID)
		return coach.Incoming{}, i18n.T(ctx, "tg.file.failed"), nil
	}
	out.Attachments = []conversations.Attachment{stored}
	return out, "", nil
}
