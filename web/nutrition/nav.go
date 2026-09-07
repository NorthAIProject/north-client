package nutrition

import (
	"context"

	"github.com/NorthAIProject/north-client/web/shared/layout"
)

// nav highlights Fitness for every nutrition page — Nutrition is a Fitness
// sub-tab (like Training/Form check/Activity/Calculator), not its own
// top-level section.
func nav(ctx context.Context) []layout.NavGroup { return layout.BuildNav(ctx, "/app/fitness") }
