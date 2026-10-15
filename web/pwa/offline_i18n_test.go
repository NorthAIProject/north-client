package pwa

import (
	"regexp"
	"strings"
	"testing"
)

// The offline page is precached once and served for every failed navigation
// after, so it cannot be rendered per request: it carries every language and
// the browser picks. These tests hold the two halves of that together.

func offlineSource(t *testing.T) string {
	t.Helper()
	raw, err := Files.ReadFile("offline.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Without the script — or for an English reader — the markup itself is the
// page, so it has to be complete English.
func TestOfflinePageMarkupIsEnglish(t *testing.T) {
	body := offlineSource(t)
	for _, want := range []string{`<html lang="en">`, "You're offline", "Try again"} {
		if !strings.Contains(body, want) {
			t.Errorf("offline.html markup lost %q", want)
		}
	}
}

// Every element the script rewrites has copy in every other language, or a
// reader would get half a page in their language and half in English.
func TestOfflinePageCoversEveryLanguage(t *testing.T) {
	body := offlineSource(t)

	marked := regexp.MustCompile(`data-i18n="([a-z]+)"`).FindAllStringSubmatch(body, -1)
	if len(marked) == 0 {
		t.Fatal("offline.html marks nothing for translation")
	}

	for _, tag := range []string{`es:`, `"pt-PT":`, `"pt-BR":`} {
		start := strings.Index(body, tag+" {")
		if start < 0 {
			t.Errorf("offline.html has no copy for %s", tag)
			continue
		}
		block := body[start:]
		block = block[:strings.Index(block, "}")]
		for _, m := range append(marked, []string{"", "doc"}) {
			if !strings.Contains(block, m[1]+`: "`) {
				t.Errorf("%s copy is missing %q", tag, m[1])
			}
		}
	}
}

// A bare "pt" is Brazilian, as in i18n.Negotiate; two paths disagreeing about
// it would show one person two languages.
func TestOfflinePagePicksLikeTheServer(t *testing.T) {
	body := offlineSource(t)
	for _, rule := range []string{
		`l === "pt" || l === "pt-br") return "pt-BR"`,
		`l.indexOf("pt-") === 0) return "pt-PT"`,
		`navigator`,
	} {
		if !strings.Contains(body, rule) {
			t.Errorf("offline.html language picking lost %q", rule)
		}
	}
}
