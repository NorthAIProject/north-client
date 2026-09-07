package mcpauth

import "testing"

// Redirect URI validation is where a bug becomes a token-stealing open
// redirect, so this is a table rather than a few spot checks.
//
// The rule is an exact string match against what the client registered, with
// one deliberate exception: RFC 8252 section 7.3 lets a native client's
// loopback port vary, because Claude Code binds whatever port is free when it
// starts the callback listener. That exception must not leak to any other
// host.
func TestMatchRedirectURI(t *testing.T) {
	registered := []string{
		"https://claude.ai/api/mcp/auth_callback",
		"http://localhost:8123/callback",
		"http://127.0.0.1:41000/cb",
	}

	accepted := map[string]string{
		"an exact https match":       "https://claude.ai/api/mcp/auth_callback",
		"an exact loopback":          "http://localhost:8123/callback",
		"a different localhost port": "http://localhost:54321/callback",
		"a different 127.0.0.1 port": "http://127.0.0.1:9999/cb",
		"loopback with no port":      "http://localhost/callback",
	}
	for name, uri := range accepted {
		t.Run("accepts "+name, func(t *testing.T) {
			if _, err := MatchRedirectURI(registered, uri); err != nil {
				t.Errorf("MatchRedirectURI(%q) = %v, want it accepted", uri, err)
			}
		})
	}

	rejected := map[string]string{
		// Not registered at all.
		"an unregistered host": "https://evil.example/cb",
		"an unregistered path": "https://claude.ai/api/mcp/other",
		"an empty uri":         "",
		"a relative uri":       "/callback",
		"a bare path":          "callback",

		// The port exception is for loopback only. A registered non-loopback
		// URI must match exactly, port included.
		"a different port on a registered host": "https://claude.ai:8443/api/mcp/auth_callback",

		// Loopback by name only. A public host that resolves to a loopback
		// address is still a public host, and 127.0.0.1.nip.io is a real
		// service that does exactly this.
		"a hostname that merely looks loopback": "http://127.0.0.1.nip.io:8123/callback",
		"a subdomain of localhost":              "http://x.localhost:8123/callback",

		// Plain http off-loopback. Registering it would put a token on the
		// wire in clear.
		"plain http on a public host": "http://claude.ai/api/mcp/auth_callback",

		// RFC 6749 section 3.1.2 forbids a fragment in a redirect URI, and the
		// authorization response appends its own query.
		"a fragment": "https://claude.ai/api/mcp/auth_callback#x",

		// Schemes that execute rather than navigate.
		"javascript": "javascript:alert(1)",
		"data":       "data:text/html,x",
		"file":       "file:///etc/passwd",

		// Path traversal, which must not normalise into a registered path.
		"traversal":         "https://claude.ai/api/mcp/../../admin",
		"encoded traversal": "https://claude.ai/api/mcp/%2e%2e/admin",

		// A prefix of a registered URI, and a registered URI with something
		// appended. Neither is the registered URI.
		"a prefix of a registered path": "https://claude.ai/api/mcp",
		"a suffix on a registered path": "https://claude.ai/api/mcp/auth_callback/extra",

		// Userinfo and a different case of host are both ways to smuggle a
		// different destination past a naive comparison.
		"userinfo":       "https://claude.ai@evil.example/api/mcp/auth_callback",
		"an added query": "https://claude.ai/api/mcp/auth_callback?x=1",
	}
	for name, uri := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			if _, err := MatchRedirectURI(registered, uri); err == nil {
				t.Errorf("MatchRedirectURI(%q) was accepted, want it rejected", uri)
			}
		})
	}
}

// A client that registered no loopback URI gets no loopback exception. The
// exception is a relaxation of a URI the client already registered, not a
// standing permission to redirect to localhost.
func TestTheLoopbackExceptionNeedsARegisteredLoopbackURI(t *testing.T) {
	registered := []string{"https://claude.ai/api/mcp/auth_callback"}

	for _, uri := range []string{
		"http://localhost:8123/callback",
		"http://127.0.0.1:8123/callback",
	} {
		if _, err := MatchRedirectURI(registered, uri); err == nil {
			t.Errorf("MatchRedirectURI(%q) was accepted against a client with no "+
				"registered loopback URI", uri)
		}
	}
}

// The returned URI is the one the client sent, not the registered spelling —
// the port it is actually listening on is the point of the exception.
func TestMatchReturnsThePresentedLoopbackURI(t *testing.T) {
	registered := []string{"http://127.0.0.1:41000/cb"}
	const presented = "http://127.0.0.1:52001/cb"

	got, err := MatchRedirectURI(registered, presented)
	if err != nil {
		t.Fatalf("MatchRedirectURI: %v", err)
	}
	if got != presented {
		t.Errorf("matched URI is %q, want the presented %q", got, presented)
	}
}

// Registration validation is the other half: a URI that would be unsafe to
// redirect to must be refused when it is registered, not when it is used.
func TestValidateRedirectURIForRegistration(t *testing.T) {
	valid := []string{
		"https://claude.ai/api/mcp/auth_callback",
		"http://localhost:8123/callback",
		"http://127.0.0.1:41000/cb",
		"http://[::1]:8123/cb",
		"https://example.test/a/b/c",
	}
	for _, uri := range valid {
		if err := ValidateRedirectURI(uri); err != nil {
			t.Errorf("ValidateRedirectURI(%q) = %v, want it accepted", uri, err)
		}
	}

	invalid := []string{
		"",
		"/relative",
		"http://claude.ai/cb",
		"javascript:alert(1)",
		"data:text/html,x",
		"https://claude.ai/cb#frag",
		"https://claude.ai/../cb",
		"https://user:pw@claude.ai/cb",
		"https:///cb",
	}
	for _, uri := range invalid {
		if err := ValidateRedirectURI(uri); err == nil {
			t.Errorf("ValidateRedirectURI(%q) was accepted, want it rejected", uri)
		}
	}
}
