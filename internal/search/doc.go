// Package search turns what a user typed into something Postgres can rank.
//
// It owns two things and deliberately nothing else: the normalisation every
// query passes through before it reaches SQL, and the exact tsvector/tsquery
// expressions that the migrations built indexes for. Both exist in one place so
// there is one thing to audit and one thing to keep in step with the schema.
//
// There are no embeddings here, and that is a decision rather than a gap.
// Full-text ranking is deterministic: a result can be shown to the user with
// the matched terms marked, and when it retrieves the wrong thing the reason is
// legible. A vector index answers "why did it pick that?" with a number.
// Postgres already ships the ranking, so this costs no dependency and no
// per-write call to a provider. The day a real query is shown to fail here is
// the day to add embeddings beside this — pgvector is already in the image.
package search
