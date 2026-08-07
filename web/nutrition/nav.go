package nutrition

import "github.com/NorthAIProject/north-client/web/shared/layout"

// nav highlights Fitness for every nutrition page — Nutrition is a Fitness
// sub-tab (like Training/Form check/Activity/Calculator), not its own
// top-level section.
func nav() []layout.NavItem { return layout.BuildNav("/app/fitness") }
