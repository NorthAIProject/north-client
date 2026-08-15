package connections

import (
	"fmt"
	"strings"
)

// Setup is everything a user needs to point one agent at North: the config a
// person edits, and the prompt a person hands to the agent instead.
//
// Both carry a live credential, so a Setup is built only in the response to the
// request that created the connection and is never stored.
type Setup struct {
	Kind  ClientKind
	URL   string
	Token string

	// ConfigLabel names the file the snippet belongs in, and ConfigLang is the
	// syntax it is written in — used for the copy block's heading and nothing
	// else.
	ConfigLabel string
	ConfigLang  string
	Config      string

	// Safe is the same configuration with the token read from an environment
	// variable instead of written into the file, and Export is the line that
	// sets it. Empty for clients whose config is not a file somebody commits.
	//
	// Not an advanced option: .mcp.json lives in a project root and is routinely
	// committed, so offering only the literal form is how a token reaches a
	// public repository.
	SafeLabel string
	SafeLang  string
	Safe      string
	Export    string

	// SafeNote explains why the second form exists, which differs by client:
	// for Claude Code it is an alternative to a literal key in a committed
	// file, for Codex it is the only form there is.
	SafeNote string

	// Prompt is the client-agnostic instruction to paste into an agent that is
	// already running, for people who would rather not open a config file.
	Prompt string
}

// PlaceholderToken stands in for a real key when the settings page shows what
// setup will look like before anybody has created one.
//
// Deliberately not shaped like a real token. A convincing placeholder is how
// somebody copies the preview, pastes it into a config, and spends an hour on a
// key that never existed — so this says what it is, loudly, in the one place a
// person's eye lands when they check whether they pasted the right thing.
//
// Lives here rather than in the template so there is exactly one of it, and so
// a preview can never be built from a token the template invented.
const PlaceholderToken = "PASTE_YOUR_KEY_HERE__create_one_below"

// Preview renders the setup for a client nobody has a key for yet.
//
// The same instructions the real thing produces, with PlaceholderToken where
// the credential goes. It exists so the page can answer "what am I signing up
// for" without issuing a credential to answer it — creating a key to find out
// what setup looks like leaves a live credential behind for a question.
func (s *Service) Preview(kind ClientKind) Setup {
	return s.Instructions(kind, PlaceholderToken)
}

// Instructions renders the setup for one issued token.
//
// The token is a parameter rather than a field on Connection because a
// Connection cannot carry one — see the comment on that type.
func (s *Service) Instructions(kind ClientKind, token string) Setup {
	url := s.baseURL + "/mcp"

	setup := Setup{
		Kind:   kind,
		URL:    url,
		Token:  token,
		Prompt: setupPrompt(url, token),
	}

	switch kind {
	case ClientClaudeCode:
		// The CLI first: it needs no file, no path, and no JSON, which is the
		// whole difficulty for someone who has not edited one before.
		setup.ConfigLabel = "one command"
		setup.ConfigLang = "bash"
		setup.Config = fmt.Sprintf(`claude mcp add --transport http north %s \
  --header "Authorization: Bearer %s"`, url, token)

		setup.SafeLabel = ".mcp.json"
		setup.SafeLang = "json"
		setup.SafeNote = "If that file lives in a git repository, use this version instead and keep " +
			"the key in your environment — .mcp.json is committed more often than people expect."
		setup.Safe = fmt.Sprintf(`{
  "mcpServers": {
    "north": {
      "type": "http",
      "url": %q,
      "headers": {
        "Authorization": "Bearer ${NORTH_MCP_TOKEN}"
      }
    }
  }
}`, url)
		setup.Export = "export NORTH_MCP_TOKEN=" + token

	case ClientCodex:
		// Codex reads the credential from a named environment variable rather
		// than taking it inline, so its config file never contains the key.
		// That makes this the one client where the git-safe form is the only
		// form — there is no literal variant to offer or to warn about.
		//
		// Verified against codex-cli 0.147.0: `codex mcp add --url` writes
		// exactly the TOML below, and the flag is --bearer-token-env-var. There
		// is no --header option for HTTP servers.
		setup.ConfigLabel = "one command"
		setup.ConfigLang = "bash"
		setup.Config = fmt.Sprintf(
			`codex mcp add north --url %s --bearer-token-env-var NORTH_MCP_TOKEN`, url)

		setup.SafeLabel = "~/.codex/config.toml"
		setup.SafeLang = "toml"
		setup.SafeNote = "That command writes this, which you can also write yourself. Codex reads " +
			"the key from the environment, so the file never contains it."
		setup.Safe = fmt.Sprintf(`[mcp_servers.north]
url = %q
bearer_token_env_var = "NORTH_MCP_TOKEN"`, url)
		setup.Export = "export NORTH_MCP_TOKEN=" + token

	case ClientHermes:
		setup.ConfigLabel = "shell"
		setup.ConfigLang = "bash"
		setup.Config = fmt.Sprintf(`hermes mcp add north --url %s --auth header
# When prompted for the header:
# Authorization: Bearer %s`, url, token)

	default:
		// Anything else speaks the protocol but not a config format North can
		// guess, so give it the two facts every MCP client needs and stop.
		setup.ConfigLabel = "connection details"
		setup.ConfigLang = "text"
		setup.Config = fmt.Sprintf(`Transport: streamable HTTP
URL:       %s
Header:    Authorization: Bearer %s`, url, token)
	}

	return setup
}

// setupPrompt is the text a user pastes into an agent so the agent edits its
// own configuration.
//
// It repeats the two rules from skills/north-connect/SKILL.md that an agent
// left to itself gets wrong — credentials in headers rather than URLs, and one
// registration rather than several — and it names the read-only check to run,
// because an agent that verifies a new connection by logging a check-in has
// written to the user's record to prove it could.
func setupPrompt(url, token string) string {
	// One line per paragraph, deliberately unwrapped.
	//
	// This text is displayed in a narrow column that soft-wraps it, so hard
	// breaks sized for an 80-column terminal wrapped a second time and left
	// orphans mid-sentence — "Do not" alone on a line, then "register North a
	// second time…". Nothing downstream wants the breaks either: the string is
	// pasted into an agent, where line length carries no meaning.
	//
	// The transport block keeps its own lines, because the alignment there is
	// the point.
	return strings.Join([]string{
		`Add an MCP server called "north" to your own configuration, then confirm it works.`,
		``,
		`  Transport: streamable HTTP`,
		`  URL:       ` + url,
		`  Header:    Authorization: Bearer ` + token,
		``,
		`Put the credential in a header, never in the URL — URLs end up in logs. Do not register North a second time under another name; duplicate tool names across MCP servers make tool selection unpredictable.`,
		``,
		`Then verify by listing the tools: you should see search_goals, list_check_ins, search_knowledge and ask_coach. Report what you found. Do not call any tool that writes (create_check_in, add_goal_update, ask_coach, calculate_macros) as part of this check.`,
		``,
		`Once connected: every call acts as one account — mine. Ask before writing, and when you use a passage from search_documents, quote the ref it gave you rather than inventing one.`,
	}, "\n")
}
