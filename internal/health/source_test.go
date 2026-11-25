package health

import (
	"errors"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// iOS builds never name a source, so absent must stay Apple Health; anything
// other than the two phone stores is refused rather than stored under a typo.
func TestDeviceSource(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"":               SourceAppleHealth,
		"apple_health":   SourceAppleHealth,
		"health_connect": SourceHealthConnect,
	} {
		got, err := deviceSource(in)
		if err != nil || got != want {
			t.Errorf("deviceSource(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"manual", "strava", "Health_Connect", "garmin"} {
		if _, err := deviceSource(in); !errors.Is(err, apperr.ErrValidation) {
			t.Errorf("deviceSource(%q) error = %v, want a validation error", in, err)
		}
	}
}
