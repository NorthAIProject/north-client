package auth

import (
	"slices"
	"testing"
)

// The Beta build is its own app, so its Apple identity tokens and Google ID
// tokens carry a different audience from the App Store build's. Both must
// sign in against the same server.
func TestNativeAudiencesAcceptEveryConfiguredBuild(t *testing.T) {
	t.Parallel()

	apple := newAppleAuth(" com.fernandocorreia.khepri , com.fernandocorreia.khepri.beta ,, ")
	want := []string{"com.fernandocorreia.khepri", "com.fernandocorreia.khepri.beta"}
	if !slices.Equal(apple.bundleIDs, want) {
		t.Fatalf("apple audiences = %q, want %q", apple.bundleIDs, want)
	}

	google := newGoogleOAuth("", "", "prod.apps.googleusercontent.com,beta.apps.googleusercontent.com", "https://kheprios.com")
	if len(google.nativeClients) != 2 {
		t.Fatalf("google native clients = %q, want both", google.nativeClients)
	}
}

// Native Google sign-in used to be dropped whenever the web redirect flow had
// no client secret, because the constructor returned before reading it.
func TestNativeGoogleSignInDoesNotNeedTheWebFlow(t *testing.T) {
	t.Parallel()

	google := newGoogleOAuth("", "", "ios.apps.googleusercontent.com", "https://kheprios.com")
	if google.enabled() {
		t.Error("web flow enabled without a client secret")
	}
	if len(google.nativeClients) != 1 {
		t.Error("native client dropped because the web flow is off")
	}
}
