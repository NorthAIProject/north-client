package weekly

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n/fieldmsg"
)

func TestFieldMessagesMatchTheCatalogue(t *testing.T) {
	t.Parallel()
	for _, m := range fieldmsg.Mismatches(fieldMessages) {
		t.Error(m)
	}
}
