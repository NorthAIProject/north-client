package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
)

func TestAppleAppSiteAssociationNamesEveryBuild(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	auth.AppleAppSiteAssociation("84X9WYBF36", "com.fernandocorreia.khepri,com.fernandocorreia.khepri.beta").
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/apple-app-site-association", nil))

	want := `{"webcredentials":{"apps":["84X9WYBF36.com.fernandocorreia.khepri","84X9WYBF36.com.fernandocorreia.khepri.beta"]}}` + "\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("got %d %s, want 200 %s", rec.Code, rec.Body.String(), want)
	}
}

func TestAppleAppSiteAssociationIsAbsentUntilConfigured(t *testing.T) {
	t.Parallel()

	for name, h := range map[string]http.HandlerFunc{
		"no team":    auth.AppleAppSiteAssociation("", "com.fernandocorreia.khepri"),
		"no bundles": auth.AppleAppSiteAssociation("84X9WYBF36", ""),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/apple-app-site-association", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, rec.Code)
		}
	}
}
