package icon

import "github.com/a-h/templ"

// BrandFor returns the mark for one provider or agent, keyed by the name the
// rest of the application already uses — providers.BYOProvider.Name for model
// providers, connections.ClientKind for agents.
//
// A switch here rather than a seven-way if at each call site, and keyed by the
// existing name rather than a new enum, so adding a provider to the catalogue
// is still a one-line change and a provider with no mark yet degrades to the
// generic plug instead of failing to render.
//
// Every mark here is the vendor's own artwork, vendored verbatim; none is a
// redrawing. See the individual brand_*.templ files.
func BrandFor(name string) templ.Component {
	switch name {
	// Claude Code gets Claude's mark rather than Anthropic's. They are
	// different pieces of artwork, and this page is asking which agent somebody
	// runs, not which company published it.
	case "claude_code":
		return BrandClaude()
	case "anthropic":
		return BrandAnthropic()
	case "openai", "codex":
		return BrandOpenAI()
	// Grok's mark rather than xAI's corporate X — same reasoning as Claude
	// above, and the catalogue already labels this one "xAI (Grok)".
	case "xai", "grok":
		return BrandGrok()
	case "nvidia":
		return BrandNvidia()
	case "gemini":
		return BrandGemini()
	case "openrouter":
		return BrandOpenRouter()
	case "hermes":
		return BrandHermes()
	default:
		// "other" — any MCP client North has no snippet for, and any provider
		// added to the catalogue before its mark is drawn.
		return Icon("plug")()
	}
}
