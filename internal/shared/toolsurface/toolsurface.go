// Package toolsurface carries which surface is invoking a capability.
//
// It is its own package because both ends need it and neither may import the
// other: internal/agent imports the feature slices, and internal/coach is one
// of the things it reaches. A shared context key is the smallest thing that
// lets the coach and the MCP server label their calls without either of them
// depending on the other.
package toolsurface

import "context"

const (
	Coach = "coach"
	MCP   = "mcp"
)

// key namespaces the value. Unexported so no other package can write here by
// accident.
type key struct{}

// With labels which surface is invoking.
//
// On the context rather than fixed when the registry is built, because one
// registry serves both surfaces and cannot know from its own wiring which of
// them is calling.
func With(ctx context.Context, surface string) context.Context {
	return context.WithValue(ctx, key{}, surface)
}

// From reads the label, or empty when nothing set one. An unlabelled call
// records the uncertainty rather than guessing.
func From(ctx context.Context) string {
	surface, _ := ctx.Value(key{}).(string)
	return surface
}
