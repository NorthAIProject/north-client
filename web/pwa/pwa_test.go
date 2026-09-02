package pwa

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestHasInstallFields(t *testing.T) {
	raw, err := Files.ReadFile("manifest.webmanifest")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "short_name", "start_url", "display", "icons", "theme_color", "background_color"} {
		if m[key] == nil {
			t.Errorf("manifest missing %q", key)
		}
	}
	if m["display"] != "standalone" {
		t.Errorf("display = %v, want standalone", m["display"])
	}
	if m["start_url"] != "/app" {
		t.Errorf("start_url = %v, want /app", m["start_url"])
	}
}

func TestServiceWorkerHasAFetchHandler(t *testing.T) {
	body := swSource(t)
	for _, want := range []string{
		`addEventListener("fetch"`,
		`event.respondWith`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sw.js missing %q", want)
		}
	}
}

// The worker may cache the static shell. It may never cache anything that
// carries state.
//
// This replaced a blanket "do not add a cache" assertion. The reason for that
// rule was never that caching is wrong — it was that a stale check-in form or
// a replayed coach stream is worse than a failed request. That reason still
// holds and is what these cases pin; what changed is that the stylesheet and
// the vendored scripts are not state, and refusing to cache them cost Khepri
// any offline shell at all.
func TestServiceWorkerNeverCachesState(t *testing.T) {
	body := swSource(t)

	// Streams, mutations and HTMX partials are handed back to the browser
	// before any cache is consulted.
	for _, want := range []string{
		`text/event-stream`,
		`request.method !== "GET"`,
		`HX-Request`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sw.js no longer bypasses %q", want)
		}
	}

	// A navigation is answered from the network or from the offline page, and
	// the real response is never written to the cache. Catching this by
	// construction: the only cache.put in the file belongs to the /assets/
	// branch.
	if strings.Count(body, "cache.put") != 1 {
		t.Errorf("cache.put appears %d times; the only one should be the static-asset branch",
			strings.Count(body, "cache.put"))
	}
	if !strings.Contains(body, `url.pathname.startsWith("/assets/")`) {
		t.Error("sw.js no longer restricts caching to /assets/")
	}
}

// An offline page is useless if the worker did not manage to store it before
// the network went away.
func TestServiceWorkerPrecachesTheOfflinePage(t *testing.T) {
	body := swSource(t)

	if !strings.Contains(body, `"/offline.html"`) {
		t.Error("sw.js does not reference the offline page")
	}
	if !strings.Contains(body, `addEventListener("install"`) {
		t.Error("sw.js has no install handler, so nothing is precached")
	}
	if !strings.Contains(body, `request.mode === "navigate"`) {
		t.Error("sw.js does not special-case navigations, so the offline page is never shown")
	}
}

// A cache that is never retired is how somebody stays pinned to a deleted
// asset after a deploy.
func TestServiceWorkerRetiresOldCaches(t *testing.T) {
	body := swSource(t)

	if !strings.Contains(body, "caches.delete") {
		t.Error("sw.js never deletes an old cache")
	}
	if !strings.Contains(body, `addEventListener("activate"`) {
		t.Error("sw.js has no activate handler")
	}
	if !strings.Contains(body, "clients.claim") {
		t.Error("sw.js does not claim open clients, so a new worker waits for every tab to close")
	}
}

// The offline page has to stand alone. It is shown precisely when requests are
// failing, so anything it links to is a request that has already failed once.
func TestOfflinePageReferencesNoOtherFiles(t *testing.T) {
	raw, err := Files.ReadFile("offline.html")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	for _, forbidden := range []string{"<link rel=\"stylesheet", "<script src", "/assets/"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("offline.html references %q; it must be self-contained", forbidden)
		}
	}
	if !strings.Contains(body, "<style>") {
		t.Error("offline.html has no inline styles, so it will render unstyled offline")
	}
}

// A nudge arrives as a push event and leaves as a tap on /open, which is what
// attributes the return to the channel. Both halves have to be in the worker
// for the measurement to mean anything.
func TestServiceWorkerHandlesPushAndTheTap(t *testing.T) {
	body := swSource(t)
	for _, want := range []string{
		`addEventListener("push"`,
		`showNotification(`,
		`addEventListener("notificationclick"`,
		`clients.openWindow(`,
		`/assets/js/shared/push.js`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sw.js missing %q", want)
		}
	}
}

func swSource(t *testing.T) string {
	t.Helper()
	raw, err := Files.ReadFile("sw.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
