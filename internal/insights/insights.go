// Package insights is the review half of the application.
//
// The domain pages — /app/care, /app/mind, /app/goals — are for doing: they
// carry the forms that log a glass of water or close a milestone. These pages
// carry none. They exist to answer "how has this been going", over a window
// the reader chooses, and every one of them is read-only.
//
// Keeping the two apart is what stops either from bloating. A page that both
// logs and analyses ends up doing neither well, and the analysis is the part
// that wants a time range, a dozen charts, and no write path at all.
//
// It owns no repository. Like dashboard, it composes the slices that own the
// data; unlike dashboard, it asks them for a whole window rather than for
// today.
package insights
