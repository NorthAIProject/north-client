package mcpauth

import (
	"context"
	"strings"
	"testing"
)

func render(t *testing.T, f ConsentForm) string {
	t.Helper()

	var b strings.Builder
	if err := Page(f).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func signedOut() ConsentForm {
	return ConsentForm{
		RequestID:    "11111111-2222-3333-4444-555555555555",
		ClientName:   "Claude Code",
		RedirectHost: "claude.ai",
		Account:      AccountForm{Mode: "signup", GoogleEnabled: true, PasskeyEnabled: true},
	}
}

// The novelty of this screen is that somebody with no account can finish on
// it. If the account block is missing, the page is just the old flow with a
// nicer border.
func TestTheSignedOutScreenCanCreateAnAccount(t *testing.T) {
	out := render(t, signedOut())

	for _, want := range []string{
		`name="display_name"`,
		`name="email"`,
		`name="password"`,
		`name="timezone"`,
		"/oauth/authorize/account?request_id=11111111-2222-3333-4444-555555555555",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the signed-out screen is missing %q", want)
		}
	}

	// One password field. This is a conversion surface, and a mistyped
	// password is recoverable by reset where a mistyped email is not.
	if n := strings.Count(out, `type="password"`); n != 1 {
		t.Errorf("the screen has %d password fields, want 1", n)
	}

	// The timezone is posted from the browser because the server cannot know
	// it, and a coach that schedules anything needs it.
	if !strings.Contains(out, "Intl.DateTimeFormat().resolvedOptions().timeZone") {
		t.Error("the screen does not capture a timezone")
	}
}

// Registration is open, so the client's name is chosen by whoever registered
// it. templ escapes by default; this fails if anybody ever reaches for
// templ.Raw on that string.
func TestTheClientNameIsEscaped(t *testing.T) {
	f := signedOut()
	f.ClientName = `<script>alert(1)</script>`

	out := render(t, f)

	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Error("the client name is rendered as markup")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("the client name does not appear escaped, so it may not appear at all")
	}
}

// The name above it can claim anything. The host is where the grant actually
// goes, and it is the only line on the page a hostile registration cannot
// control — which makes it the anti-phishing line.
func TestTheDestinationIsAlwaysShown(t *testing.T) {
	web := render(t, signedOut())
	if !strings.Contains(web, "claude.ai") {
		t.Error("the screen does not say where the access is going")
	}

	f := signedOut()
	f.IsLoopback = true
	f.RedirectHost = "this computer"
	local := render(t, f)
	if !strings.Contains(local, "this computer") {
		t.Error("a loopback destination is not described in plain words")
	}
	// "localhost" is jargon; the distinction a person needs is between their
	// own machine and the internet.
	if strings.Contains(local, "localhost") {
		t.Error("the screen shows localhost rather than plain words")
	}
}

// A crafted authorize link plus an auto-submit would be a one-click grant, and
// it is the one thing on this page that could go wrong silently.
func TestApproveIsADeliberateAct(t *testing.T) {
	f := signedOut()
	f.Email = "fernando@north.test"

	out := render(t, f)

	if !strings.Contains(out, `value="approve"`) {
		t.Fatal("there is no Approve button")
	}
	for _, forbidden := range []string{
		"$el.submit()",
		".submit()",
		"hx-trigger=\"load\"",
		"autofocus",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the consent form contains %q, which could approve without a click", forbidden)
		}
	}
}

// Every form on this page posts to the session group, so every form needs the
// same CSRF token the rest of the application uses.
func TestEveryFormCarriesACSRFToken(t *testing.T) {
	signedIn := signedOut()
	signedIn.Email = "fernando@north.test"

	for name, f := range map[string]ConsentForm{
		"signed out": signedOut(),
		"signed in":  signedIn,
	} {
		out := render(t, f)
		forms := strings.Count(out, "<form")
		tokens := strings.Count(out, `name="csrf_token"`)
		if tokens < forms {
			t.Errorf("%s: %d forms but %d CSRF inputs", name, forms, tokens)
		}
	}
}

// Read-only is offered, and defaults to what the client asked for. A person
// may narrow the grant here; Approve refuses to widen it.
func TestTheReadOnlyToggleReflectsTheRequest(t *testing.T) {
	signedIn := signedOut()
	signedIn.Email = "fernando@north.test"

	// Read the toggle's own tag rather than the whole page, so the assertion
	// does not depend on where an attribute happens to be serialised.
	toggle := func(out string) string {
		start := strings.Index(out, `name="read_only"`)
		if start < 0 {
			t.Fatal("there is no read-only toggle")
		}
		open := strings.LastIndex(out[:start], "<input")
		end := strings.Index(out[start:], ">")
		return out[open : start+end]
	}

	full := toggle(render(t, signedIn))
	if strings.Contains(full, "checked") {
		t.Errorf("read-only is pre-checked for a read-write request: %s", full)
	}

	signedIn.ReadOnlyRequested = true
	readOnlyPage := render(t, signedIn)
	if !strings.Contains(toggle(readOnlyPage), "checked") {
		t.Errorf("read-only is not pre-checked for a read-only request: %s", toggle(readOnlyPage))
	}
	// And the sentences change with it, rather than promising writes it will
	// not have.
	if !strings.Contains(readOnlyPage, "not be able to change") {
		t.Error("a read-only request does not say that nothing can be changed")
	}
}

// The error page is what a fatal failure renders instead of redirecting,
// because at that point the destination is not trustworthy.
func TestTheErrorPageRendersTheReason(t *testing.T) {
	var b strings.Builder
	const reason = "The application making this request is not registered with Khepri."
	if err := ErrorPage(reason).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	if !strings.Contains(out, reason) {
		t.Error("the error page does not say what went wrong")
	}
	// Nothing on it may lead back to the untrusted destination.
	if strings.Contains(out, "redirect_uri") {
		t.Error("the error page mentions the redirect uri")
	}
}
