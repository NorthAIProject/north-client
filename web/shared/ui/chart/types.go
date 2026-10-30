package chart

import "github.com/NorthAIProject/north-client/internal/shared/viz"

// The chart description moved to internal/shared/viz so the JSON API can build
// charts without importing this package. These aliases keep the component and
// its callers unchanged.
type (
	Variant = viz.Variant
	Dataset = viz.Dataset
	Options = viz.Options
	Data    = viz.Data
	Props   = viz.Props
)

const (
	VariantBar      = viz.VariantBar
	VariantLine     = viz.VariantLine
	VariantPie      = viz.VariantPie
	VariantDoughnut = viz.VariantDoughnut
	VariantRadar    = viz.VariantRadar
)
