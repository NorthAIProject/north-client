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
	raw, err := Files.ReadFile("sw.js")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		`addEventListener("fetch"`,
		`event.respondWith`,
		`text/event-stream`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sw.js missing %q", want)
		}
	}
	if strings.Contains(body, "cache.put") || strings.Contains(body, "caches.open") {
		t.Error("sw.js must stay network-only; do not add a cache")
	}
}
