package mcpauth

import "github.com/NorthAIProject/north-client/internal/mcpserver"

// The two discovery documents a client reads before it knows how to
// authenticate.
//
// Together they are what makes one pasted URL enough. A client given
// https://kheprios.com/mcp gets a 401 carrying a resource_metadata pointer,
// fetches the protected-resource document to learn which authorization server
// guards it, fetches that server's document to learn where to register and
// authorize, and registers itself. Nobody types anything but the URL.

// ProtectedResourceMetadata is RFC 9728: which authorization server guards
// /mcp.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceDocumentation  string   `json:"resource_documentation,omitempty"`
}

// AuthorizationServerMetadata is RFC 8414: where to register, authorize and
// exchange.
type AuthorizationServerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`

	ResponseTypesSupported []string `json:"response_types_supported"`
	GrantTypesSupported    []string `json:"grant_types_supported"`
	ScopesSupported        []string `json:"scopes_supported"`

	// CodeChallengeMethodsSupported advertises S256 and only S256. RFC 7636
	// also defines "plain", and leaving it out here is half of refusing it —
	// a client picks from this list, so a method absent from it is a method
	// never selected. The other half is the refusal in ValidateChallenge.
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`

	// TokenEndpointAuthMethodsSupported is "none": every MCP client is a
	// public client, and North issues no client secrets.
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// scopesSupported is the vocabulary both documents advertise.
func scopesSupported() []string {
	return []string{mcpserver.ScopeRead, mcpserver.ScopeReadWrite}
}

// ProtectedResource describes /mcp.
func (s *Service) ProtectedResource() ProtectedResourceMetadata {
	return ProtectedResourceMetadata{
		Resource:               s.Resource(),
		AuthorizationServers:   []string{s.baseURL},
		ScopesSupported:        scopesSupported(),
		BearerMethodsSupported: []string{"header"},

		// Where a person, rather than a client, goes to see and revoke what
		// they have connected.
		ResourceDocumentation: s.baseURL + "/app/settings/connections",
	}
}

// AuthorizationServer describes this server.
func (s *Service) AuthorizationServer() AuthorizationServerMetadata {
	return AuthorizationServerMetadata{
		Issuer:                s.baseURL,
		AuthorizationEndpoint: s.baseURL + "/oauth/authorize",
		TokenEndpoint:         s.baseURL + "/oauth/token",
		RegistrationEndpoint:  s.baseURL + "/oauth/register",
		RevocationEndpoint:    s.baseURL + "/oauth/revoke",

		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    []string{"authorization_code", "refresh_token"},
		ScopesSupported:        scopesSupported(),

		CodeChallengeMethodsSupported:     []string{ChallengeMethodS256},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	}
}

// ResourceMetadataURL is the pointer the 401 on /mcp carries.
//
// RFC 9728 puts the resource's path component into the well-known path, so a
// client resolving /mcp asks for /.well-known/oauth-protected-resource/mcp.
// The bare path is served too, because several clients still ask for it.
func (s *Service) ResourceMetadataURL() string {
	return s.baseURL + "/.well-known/oauth-protected-resource/mcp"
}
