package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderPage(t *testing.T, f ConnectForm, provider ProviderPanel) string {
	t.Helper()

	svc := connections.NewService(nil, nil, "https://north.example.com")
	previews := make([]connections.Setup, 0, len(connections.ClientKinds))
	for _, kind := range connections.ClientKinds {
		previews = append(previews, svc.Preview(kind))
	}

	var b strings.Builder
	page := ConnectionsPage(
		users.User{DisplayName: "Test"},
		nil, f, nil, connections.Setup{}, previews, provider,
		// Disabled: these cases are about the agent and provider cards, and a
		// deployment with no bot token is the shape most of them run in.
		TelegramPanel{},
	)
	if err := page.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func enabledPanel() ProviderPanel {
	return ProviderPanel{
		Enabled: true,
		Catalog: providers.Catalog,
		Form:    ProviderForm{Provider: "openrouter"},
	}
}

// The rebuild's whole point: every client's setup is legible before a key
// exists. If a snippet is missing, the page has gone back to making people
// issue a credential to find out what connecting involves.
func TestEveryClientSnippetIsVisibleBeforeAnyKeyExists(t *testing.T) {
	html := renderPage(t, ConnectForm{}, enabledPanel())

	for _, kind := range connections.ClientKinds {
		if !strings.Contains(html, `data-tui-tabs-value="`+string(kind)+`"`) {
			t.Errorf("no tab for %s", kind)
		}
	}

	// The verified per-client forms, not a generic blob.
	for _, want := range []string{
		"claude mcp add --transport http north",
		"codex mcp add north --url",
		"hermes mcp add north --url",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page does not carry %q", want)
		}
	}
}

// A preview must never look like a working credential.
func TestPreviewsShowThePlaceholderAndNotARealKey(t *testing.T) {
	html := renderPage(t, ConnectForm{}, enabledPanel())

	if !strings.Contains(html, connections.PlaceholderToken) {
		t.Error("the previews do not carry the placeholder token")
	}
	if strings.Contains(html, "nk_") {
		t.Error("something key-shaped reached a page where no key has been issued")
	}
}

// A failed submission must come back on the client the person chose, not reset
// to the first tab and lose it.
func TestTheChosenClientStaysSelected(t *testing.T) {
	html := renderPage(t, ConnectForm{Kind: connections.ClientCodex}, enabledPanel())

	codex := `data-tui-tabs-value="codex" data-tui-tabs-state="active"`
	if !strings.Contains(html, codex) {
		t.Errorf("codex is not the active tab; want a trigger and panel carrying %q", codex)
	}
}

// Without JavaScript the templUI tabs render every inactive panel `hidden`, so
// the page needs its own fallback or three of the four clients are unreachable.
func TestTheNoScriptFallbackCarriesEveryClient(t *testing.T) {
	html := renderPage(t, ConnectForm{}, enabledPanel())

	_, after, found := strings.Cut(html, "<noscript>")
	if !found {
		t.Fatal("no noscript fallback")
	}
	fallback, _, _ := strings.Cut(after, "</noscript>")

	for _, kind := range connections.ClientKinds {
		if !strings.Contains(fallback, kind.Label()) {
			t.Errorf("the noscript fallback omits %s", kind.Label())
		}
	}
}

// Every catalogue provider gets a tile, and the tile is a real radio so the
// form still submits with Alpine unavailable.
func TestEveryProviderGetsASelectableTile(t *testing.T) {
	html := renderPage(t, ConnectForm{}, enabledPanel())

	for _, entry := range providers.Catalog {
		if !strings.Contains(html, `name="provider" value="`+entry.Name+`"`) {
			t.Errorf("no radio tile for %s", entry.Name)
		}
	}
}

// The gateway field used to render for all six providers, which read as though
// an OpenRouter key needed a URL too. It is now driven by the same
// RequiresBaseURL the server validates against.
func TestTheGatewayFieldIsDrivenByTheCatalogue(t *testing.T) {
	html := renderPage(t, ConnectForm{}, enabledPanel())

	if !strings.Contains(html, `x-show="needsURL.includes(provider)"`) {
		t.Error("the gateway field is not conditional on the provider")
	}

	// Seeded from the catalogue, so exactly the providers that declare
	// RequiresBaseURL appear in the Alpine state — no "hermes" literal.
	for _, entry := range providers.Catalog {
		inState := strings.Contains(html, `&#34;needsURL&#34;:[&#34;`+entry.Name+`&#34;`) ||
			strings.Contains(html, `,&#34;`+entry.Name+`&#34;]`)
		if entry.RequiresBaseURL && !inState {
			t.Errorf("%s requires a base URL but is not in the Alpine state", entry.Name)
		}
	}
}

// The mode radios once bound a boolean through x-model with :value, which
// replaced their real submitted values with "true"/"false" and left neither one
// checked on a first load — the card opened with no mode selected at all.
func TestModeRadiosKeepTheirValuesAndOneIsAlwaysChecked(t *testing.T) {
	t.Run("no credential opens on North's AI", func(t *testing.T) {
		html := renderPage(t, ConnectForm{}, enabledPanel())

		if strings.Contains(html, `:value="false"`) || strings.Contains(html, `:value="true"`) {
			t.Error("a boolean is bound over the radio's value again")
		}
		if !strings.Contains(html, `name="mode" value="north" x-model="mode" checked`) {
			t.Error(`"Use North's AI" is not checked when there is no stored credential`)
		}
		if !strings.Contains(html, `&#34;mode&#34;:&#34;north&#34;`) {
			t.Error("Alpine state does not open in north mode")
		}
	})

	t.Run("a stored credential opens on the user's own key", func(t *testing.T) {
		panel := enabledPanel()
		panel.Current = &aicreds.Credential{Provider: "hermes", KeyHint: "abcd"}
		html := renderPage(t, ConnectForm{}, panel)

		if !strings.Contains(html, `name="mode" value="own" x-model="mode" checked`) {
			t.Error(`"Use my own API key" is not checked when a credential is stored`)
		}
		if !strings.Contains(html, `&#34;mode&#34;:&#34;own&#34;`) {
			t.Error("Alpine state does not open in own mode")
		}
	})
}

// The key and model placeholders have to follow the selected tile. Leaving
// OpenRouter's sk-or-v1- shape on screen while Hermes is selected defeats the
// point of a key hint, which exists so somebody can tell at a glance whether
// they pasted the right thing.
func TestPlaceholdersFollowTheSelectedProvider(t *testing.T) {
	html := renderPage(t, ConnectForm{}, enabledPanel())

	for _, want := range []string{
		`:placeholder="hints[provider] ? hints[provider].key : &#39;&#39;"`,
		`:placeholder="hints[provider] ? hints[provider].model : &#39;&#39;"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing placeholder binding %s", want)
		}
	}

	// Every catalogue entry needs an entry in hints, or selecting it blanks the
	// placeholder instead of changing it.
	for _, entry := range providers.Catalog {
		if !strings.Contains(html, `&#34;`+entry.Name+`&#34;:{`) {
			t.Errorf("%s has no hints entry", entry.Name)
		}
	}
}

// With no encryption key the card must explain itself rather than offer a form
// it cannot honour.
func TestProviderCardExplainsItselfWhenDisabled(t *testing.T) {
	html := renderPage(t, ConnectForm{}, ProviderPanel{Enabled: false, Catalog: providers.Catalog})

	if strings.Contains(html, `name="provider" value="openrouter"`) {
		t.Error("provider tiles rendered on a deployment that cannot store keys")
	}
	if !strings.Contains(html, "no encryption key configured") {
		t.Error("the card does not say why it is unavailable")
	}
}
