package coach

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestConversationURLOmitsAnEmptyDraft(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if got := conversationURL(id, "  "); got != "/app/chat/"+id.String() {
		t.Fatalf("got %q", got)
	}
}

func TestConversationURLCarriesADraft(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got := conversationURL(id, "Help me build a habit I'll actually keep.")
	if !strings.Contains(got, "draft=") {
		t.Fatalf("missing draft query: %q", got)
	}
	if strings.Contains(got, "I'll") {
		t.Fatalf("draft was not escaped: %q", got)
	}
}

func TestClipDraftCapsALongURL(t *testing.T) {
	long := strings.Repeat("å", draftRuneCap+20)
	got := clipDraft(long)
	if got != strings.Repeat("å", draftRuneCap) {
		t.Fatalf("len = %d, want %d", len([]rune(got)), draftRuneCap)
	}
}
