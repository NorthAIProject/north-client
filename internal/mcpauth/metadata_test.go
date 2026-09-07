package mcpauth_test

import (
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/mcpauth"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
)

// The discovery documents are a contract a client reads before it can do
// anything, and one wrong URL is a client that fails with nothing useful in
// the response. They are built from configuration, so this needs no database.
func TestTheDiscoveryDocuments(t *testing.T) {
	svc := mcpauth.NewService(nil, nil, "https://north.test/")

	t.Run("the protected resource points at this deployment", func(t *testing.T) {
		doc := svc.ProtectedResource()

		if doc.Resource != "https://north.test/mcp" {
			t.Errorf("resource is %q, want the /mcp endpoint", doc.Resource)
		}
		if len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://north.test" {
			t.Errorf("authorization_servers is %v, want this origin", doc.AuthorizationServers)
		}
		if len(doc.BearerMethodsSupported) != 1 || doc.BearerMethodsSupported[0] != "header" {
			t.Errorf("bearer_methods_supported is %v, want [header]", doc.BearerMethodsSupported)
		}
		wantScopes(t, doc.ScopesSupported)
	})

	t.Run("the authorization server advertises S256 and nothing else", func(t *testing.T) {
		doc := svc.AuthorizationServer()

		// Absence is half of refusing "plain": a client picks from this list,
		// so a method missing from it is a method never selected.
		if len(doc.CodeChallengeMethodsSupported) != 1 ||
			doc.CodeChallengeMethodsSupported[0] != mcpauth.ChallengeMethodS256 {
			t.Errorf("code_challenge_methods_supported is %v, want only S256",
				doc.CodeChallengeMethodsSupported)
		}
		for _, method := range doc.CodeChallengeMethodsSupported {
			if method == "plain" {
				t.Error("the metadata advertises the plain challenge method")
			}
		}

		if len(doc.ResponseTypesSupported) != 1 || doc.ResponseTypesSupported[0] != "code" {
			t.Errorf("response_types_supported is %v, want [code]", doc.ResponseTypesSupported)
		}
		// Public clients only. Advertising anything else would invite a client
		// to send a secret this server will not honour.
		if len(doc.TokenEndpointAuthMethodsSupported) != 1 ||
			doc.TokenEndpointAuthMethodsSupported[0] != "none" {
			t.Errorf("token_endpoint_auth_methods_supported is %v, want [none]",
				doc.TokenEndpointAuthMethodsSupported)
		}

		for name, got := range map[string]string{
			"issuer":                 doc.Issuer,
			"authorization_endpoint": doc.AuthorizationEndpoint,
			"token_endpoint":         doc.TokenEndpoint,
			"registration_endpoint":  doc.RegistrationEndpoint,
			"revocation_endpoint":    doc.RevocationEndpoint,
		} {
			if got == "" {
				t.Errorf("%s is empty", name)
			}
		}
		if doc.AuthorizationEndpoint != "https://north.test/oauth/authorize" {
			t.Errorf("authorization_endpoint is %q", doc.AuthorizationEndpoint)
		}
		wantScopes(t, doc.ScopesSupported)
	})

	// A trailing slash on BASE_URL must not produce a double slash anywhere: a
	// mismatched resource value is the most common failure in this flow and it
	// surfaces as an unhelpful client-side error.
	t.Run("a trailing slash on the base url is absorbed", func(t *testing.T) {
		if got := svc.Resource(); got != "https://north.test/mcp" {
			t.Errorf("resource is %q, want no double slash", got)
		}
		if got := svc.ResourceMetadataURL(); got !=
			"https://north.test/.well-known/oauth-protected-resource/mcp" {
			t.Errorf("resource metadata url is %q", got)
		}
	})

	t.Run("both documents are valid json", func(t *testing.T) {
		for name, doc := range map[string]any{
			"protected resource":   svc.ProtectedResource(),
			"authorization server": svc.AuthorizationServer(),
		} {
			encoded, err := json.Marshal(doc)
			if err != nil {
				t.Fatalf("%s does not marshal: %v", name, err)
			}
			var round map[string]any
			if err := json.Unmarshal(encoded, &round); err != nil {
				t.Fatalf("%s does not round-trip: %v", name, err)
			}
		}
	})
}

func wantScopes(t *testing.T, got []string) {
	t.Helper()
	want := []string{mcpserver.ScopeRead, mcpserver.ScopeReadWrite}
	if len(got) != len(want) {
		t.Fatalf("scopes_supported is %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scopes_supported[%d] is %q, want %q", i, got[i], want[i])
		}
	}
}
