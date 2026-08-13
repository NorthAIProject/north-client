// Package dashboard composes today's home: check-in, active goals, the last
// coach thread, and the next training session if there is one.
//
// It owns no repository. The slices it reads own their data; this package owns
// the question "what should I do today", which is a page rather than a domain.
package dashboard
