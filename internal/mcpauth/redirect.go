package mcpauth

import (
	"net/url"
	"strings"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// loopbackHosts are the hosts RFC 8252 section 7.3 treats as loopback.
//
// Matched by name, never resolved. A hostname that resolves to a loopback
// address is still a public hostname under somebody else's control —
// 127.0.0.1.nip.io is a real service that does exactly that — and resolving
// would also make validation depend on DNS.
var loopbackHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// ValidateRedirectURI reports whether a URI is safe to register as a
// destination for an authorization code.
//
// Checked at registration so an unusable URI is refused where the client can
// still do something about it, and checked again at match time because a
// registration predates any change to these rules.
func ValidateRedirectURI(raw string) error {
	_, err := parseRedirectURI(raw)
	return err
}

// MatchRedirectURI resolves a presented redirect_uri against what a client
// registered, returning the URI to actually redirect to.
//
// An exact string match, with one exception: RFC 8252 section 7.3 lets the
// port of a loopback URI vary, because a native client binds whatever port is
// free when it starts its callback listener — Claude Code picks a new one
// every run, so requiring an exact match would mean re-registering on every
// launch.
//
// The exception is deliberately narrow. It applies only when the client
// registered a loopback URI of its own, and only to the port: the scheme, the
// host and the path must still agree. A client that registered nothing on
// loopback gets no loopback destination.
func MatchRedirectURI(registered []string, presented string) (string, error) {
	want, err := parseRedirectURI(presented)
	if err != nil {
		return "", err
	}

	for _, candidate := range registered {
		if candidate == presented {
			return presented, nil
		}
	}

	// The port-varying case. Compared on the parsed parts rather than on
	// strings with the port cut out, so ":8123" appearing in a path cannot be
	// mistaken for a port.
	if isLoopback(want) {
		for _, candidate := range registered {
			have, parseErr := parseRedirectURI(candidate)
			if parseErr != nil {
				// A registration that no longer validates is not a match. It
				// cannot be used, and skipping it is safer than repairing it
				// here.
				continue
			}
			if !isLoopback(have) {
				continue
			}
			if have.Scheme == want.Scheme &&
				have.Hostname() == want.Hostname() &&
				have.EscapedPath() == want.EscapedPath() &&
				have.RawQuery == want.RawQuery {
				// The presented URI, not the registered one: the port it is
				// listening on right now is the entire point.
				return presented, nil
			}
		}
	}

	return "", apperr.Wrap(apperr.ErrValidation, "redirect_uri is not registered for this client")
}

// parseRedirectURI parses and checks one URI, returning it for comparison.
func parseRedirectURI(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "redirect_uri is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrValidation, "redirect_uri is not a valid URI")
	}

	// Absolute only. A relative URI would resolve against whatever page the
	// browser happened to be on.
	if !parsed.IsAbs() || parsed.Host == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "redirect_uri must be an absolute http or https URI")
	}

	// RFC 6749 section 3.1.2: no fragment. The authorization response appends
	// its own query, and a fragment would also hide the real destination from
	// the consent screen.
	if parsed.Fragment != "" || strings.Contains(raw, "#") {
		return nil, apperr.Wrap(apperr.ErrValidation, "redirect_uri must not contain a fragment")
	}

	// Userinfo is how a different destination gets smuggled past a reader:
	// https://claude.ai@evil.example/cb goes to evil.example, and the consent
	// screen shows a host somebody has to be able to trust.
	if parsed.User != nil {
		return nil, apperr.Wrap(apperr.ErrValidation, "redirect_uri must not contain userinfo")
	}

	switch parsed.Scheme {
	case "https":
		// Always fine.
	case "http":
		// Loopback only. Plain http anywhere else puts an authorization code
		// on the wire in clear.
		if !isLoopback(parsed) {
			return nil, apperr.Wrap(apperr.ErrValidation,
				"redirect_uri must use https, except on loopback")
		}
	default:
		// javascript:, data:, file: and anything else that executes or reads
		// rather than navigates.
		return nil, apperr.Wrap(apperr.ErrValidation, "redirect_uri must use http or https")
	}

	// Traversal, checked on the decoded path and the raw string both: %2e%2e
	// decodes into .. and a comparison against a registered path would then
	// be made against a path that is not where the browser lands.
	if hasTraversal(parsed.Path) || hasTraversal(raw) {
		return nil, apperr.Wrap(apperr.ErrValidation, "redirect_uri must not contain a path traversal")
	}

	return parsed, nil
}

// isLoopback reports whether a URI's host is one of the loopback names.
func isLoopback(u *url.URL) bool {
	return loopbackHosts[strings.ToLower(u.Hostname())]
}

// hasTraversal reports whether a path contains a ".." segment.
//
// Segment-wise rather than a substring search, so a legitimate path like
// /a..b/cb is not refused for containing two dots.
func hasTraversal(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}
