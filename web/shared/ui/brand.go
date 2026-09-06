package ui

// ProductName is the word people see in chrome, tabs, and the coach's voice.
//
// One constant so a rename is a single edit, not eighty string literals. The
// repository, Go module, and asset filenames stay as they are.
const ProductName = "Khepri"

// SiteURL is the canonical public origin, used to build the absolute URLs that
// link previews require: og:image and og:url are fetched by a crawler that has
// no page context to resolve a relative path against.
//
// A constant rather than config because it is a brand fact, not a deployment
// one — the domain a link is shared under does not change per environment, and
// a preview card built from a localhost URL would be a broken card.
const SiteURL = "https://kheprios.com"

// Tagline is the one sentence that appears under the name in a link preview and
// in search results. Kept here so the card, the manifest, and any future
// marketing copy cannot drift apart.
const Tagline = "An AI operating system for personal growth."
