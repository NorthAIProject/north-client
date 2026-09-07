package mcpauth

import "net/url"

// toggleHref swaps the account block between signup and sign-in without
// leaving the consent screen. The request id is the only state carried.
func toggleHref(f ConsentForm) string {
	mode := "login"
	if f.Account.isLogin() {
		mode = "signup"
	}
	return "/oauth/authorize?request_id=" + url.QueryEscape(f.RequestID) + "&mode=" + mode
}

// resumeQueryEscaped is resumePath, escaped for use as a next= value.
func resumeQueryEscaped(requestID string) string {
	return url.QueryEscape(resumePath(requestID))
}

// passkeyMode tells web/assets/js/auth/passkey.js which ceremony to run.
func passkeyMode(a AccountForm) string {
	if a.isLogin() {
		return "login"
	}
	return "register"
}
