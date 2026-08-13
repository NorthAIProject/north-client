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
	Safe      string
	Export    string

	// Prompt is the client-agnostic instruction to paste into an agent that is
	// already running, for people who would rather not open a config file.
	Prompt string
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
		setup.ConfigLabel = "~/.codex/config.toml"
		setup.ConfigLang = "toml"
		setup.Config = fmt.Sprintf(`[mcp_servers.north]
url = %q
http_headers = { Authorization = "Bearer %s" }`, url, token)

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
	return strings.Join([]string{
		`Add an MCP server called "north" to your own configuration, then confirm it works.`,
		``,
		`  Transport: streamable HTTP`,
		`  URL:       ` + url,
		`  Header:    Authorization: Bearer ` + token,
		``,
		`Put the credential in a header, never in the URL — URLs end up in logs. Do not`,
		`register North a second time under another name; duplicate tool names across MCP`,
		`servers make tool selection unpredictable.`,
		``,
		`Then verify by listing the tools: you should see search_goals, list_check_ins,`,
		`search_knowledge and ask_coach. Report what you found. Do not call any tool that`,
		`writes (create_check_in, add_goal_update, ask_coach, calculate_macros) as part of`,
		`this check.`,
		``,
		`Once connected: every call acts as one account — mine. Ask before writing, and`,
		`when you use a passage from search_documents, quote the ref it gave you rather`,
		`than inventing one.`,
	}, "\n")
}
