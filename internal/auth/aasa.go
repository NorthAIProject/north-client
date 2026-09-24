package auth

import (
	"net/http"

	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// AppleAppSiteAssociation serves /.well-known/apple-app-site-association,
// the file iOS fetches to trust that an app and this domain belong together.
// Without it the app cannot create or use passkeys for this domain: the
// relying party ID is the domain, and iOS only lets an app use a domain's
// passkeys when the domain names the app here.
//
// teamID is the Apple developer team; bundleIDs is the comma-separated list
// also used for Sign in with Apple, so the Beta build is covered too. Either
// one empty serves 404, which is what iOS expects from a domain that has not
// opted in.
func AppleAppSiteAssociation(teamID, bundleIDs string) http.HandlerFunc {
	var apps []string
	for _, id := range splitList(bundleIDs) {
		apps = append(apps, teamID+"."+id)
	}

	type section struct {
		Apps []string `json:"apps"`
	}
	body := struct {
		WebCredentials section `json:"webcredentials"`
	}{WebCredentials: section{Apps: apps}}

	return func(w http.ResponseWriter, r *http.Request) {
		if teamID == "" || len(apps) == 0 {
			http.NotFound(w, r)
			return
		}
		// Apple's CDN fetches and caches this; it must be JSON at this exact
		// path, with no redirect.
		httpx.WriteJSON(w, http.StatusOK, body)
	}
}
