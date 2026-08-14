package connections_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/connections"
)

// These snippets were verified by running the real clients and reading what
// they wrote:
//
//	claude-code — claude mcp add --transport http --scope project north <url> --header "Authorization: Bearer …"
//	codex-cli 0.147.0 — codex mcp add north --url <url> --bearer-token-env-var NORTH_MCP_TOKEN
//
// Both then produced exactly the config asserted below. A wrong snippet is
// worse than no snippet: it fails on the first attempt, which is the attempt
// that decides whether somebody bothers again. Re-verify against a real install
// before changing any of this.
func instructions(t *testing.T, kind connections.ClientKind) connections.Setup {
	t.Helper()

	svc := connections.NewService(nil, nil, "https://north.example.com")
	return svc.Instructions(kind, "nk_TESTTOKEN")
}

func TestClaudeCodeConfigMatchesWhatTheClientWrites(t *testing.T) {
	setup := instructions(t, connections.ClientClaudeCode)

	if !strings.Contains(setup.Config, "claude mcp add --transport http north https://north.example.com/mcp") {
		t.Errorf("the CLI form does not match the client's own syntax:\n%s", setup.Config)
	}
	if !strings.Contains(setup.Config, `--header "Authorization: Bearer nk_TESTTOKEN"`) {
		t.Errorf("the CLI form does not pass the credential as a header:\n%s", setup.Config)
	}

	// The file form has to parse, and has to have the shape Claude Code writes.
	var parsed struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(setup.Safe), &parsed); err != nil {
		t.Fatalf("the .mcp.json form is not valid JSON: %v\n%s", err, setup.Safe)
	}

	north, ok := parsed.MCPServers["north"]
	if !ok {
		t.Fatalf("no server named north:\n%s", setup.Safe)
	}
	if north.Type != "http" {
		t.Errorf("type = %q, want http", north.Type)
	}
	if north.URL != "https://north.example.com/mcp" {
		t.Errorf("url = %q", north.URL)
	}
	if north.Headers["Authorization"] != "Bearer ${NORTH_MCP_TOKEN}" {
		t.Errorf("Authorization = %q, want the environment reference", north.Headers["Authorization"])
	}

	// The git-safe form must not contain the key, or it is not git-safe.
	if strings.Contains(setup.Safe, "nk_TESTTOKEN") {
		t.Error("the .mcp.json form contains the literal key")
	}
	if !strings.Contains(setup.Export, "nk_TESTTOKEN") {
		t.Error("the export line does not carry the key, so nothing supplies it")
	}
}

// Codex has no --header flag for HTTP servers; the credential is named, not
// inlined. Getting this wrong was the original bug in these snippets.
func TestCodexConfigMatchesWhatTheClientWrites(t *testing.T) {
	setup := instructions(t, connections.ClientCodex)

	if !strings.Contains(setup.Config, "codex mcp add north --url https://north.example.com/mcp") {
		t.Errorf("the CLI form does not match the client's own syntax:\n%s", setup.Config)
	}
	if !strings.Contains(setup.Config, "--bearer-token-env-var NORTH_MCP_TOKEN") {
		t.Errorf("the CLI form does not name the credential's environment variable:\n%s", setup.Config)
	}
	if strings.Contains(setup.Config, "--header") {
		t.Error("Codex has no --header flag for HTTP servers")
	}

	for _, want := range []string{
		"[mcp_servers.north]",
		`url = "https://north.example.com/mcp"`,
		`bearer_token_env_var = "NORTH_MCP_TOKEN"`,
	} {
		if !strings.Contains(setup.Safe, want) {
			t.Errorf("the config.toml form is missing %q:\n%s", want, setup.Safe)
		}
	}
	if strings.Contains(setup.Safe, "http_headers") {
		t.Error("http_headers is not a key Codex reads")
	}
	if strings.Contains(setup.Safe, "nk_TESTTOKEN") {
		t.Error("Codex never takes the key inline; the config must not contain it")
	}
}

// Whatever the client, the credential belongs in a header and never in the URL.
func TestNoSnippetPutsTheKeyInTheURL(t *testing.T) {
	for _, kind := range connections.ClientKinds {
		t.Run(string(kind), func(t *testing.T) {
			setup := instructions(t, kind)

			for name, text := range map[string]string{
				"config": setup.Config,
				"safe":   setup.Safe,
				"prompt": setup.Prompt,
			} {
				if strings.Contains(text, "/mcp?") || strings.Contains(text, "token=") {
					t.Errorf("%s puts the credential in the URL:\n%s", name, text)
				}
			}
		})
	}
}

// The prompt is handed to a model, so the rules it carries are the only ones
// that will be followed.
func TestTheSetupPromptCarriesTheRulesThatMatter(t *testing.T) {
	setup := instructions(t, connections.ClientOther)

	for _, want := range []string{
		"https://north.example.com/mcp",
		"nk_TESTTOKEN",
		"never in the URL",
		"Do not",
	} {
		if !strings.Contains(setup.Prompt, want) {
			t.Errorf("the prompt does not mention %q", want)
		}
	}

	// It names read-only tools for the check, and warns off the writing ones.
	// A verification step that logs a check-in has changed the user's record to
	// prove it could.
	for _, tool := range []string{"create_check_in", "add_goal_update", "ask_coach", "calculate_macros"} {
		if !strings.Contains(setup.Prompt, tool) {
			t.Errorf("the prompt does not warn against calling %s", tool)
		}
	}
}
