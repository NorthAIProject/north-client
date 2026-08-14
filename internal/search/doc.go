// Package search turns what a user typed into something Postgres can rank, and
// fuses several rankings into one.
//
// It owns three things and deliberately nothing else: the normalisation every
// query passes through before it reaches SQL, the exact tsvector/tsquery
// expressions that the migrations built indexes for, and the rank fusion that
// combines a full-text ranking with a vector one. All three are here so there is
// one thing to audit and one thing to keep in step with the schema.
//
// Embeddings were once absent by decision. They are not any more: chunk vectors
// live in pgvector (see the chunk_embeddings migration) and internal/documents
// queries them. What survives of the original reasoning is the arrangement —
// full-text is the floor, not the fallback. It is deterministic, it can show the
// reader which terms matched, and when it retrieves the wrong thing the reason is
// legible, where a vector index answers "why did it pick that?" with a number.
// So the vector side is fused in rather than substituted, and a deployment with
// no embedding provider still has a complete search rather than a degraded one.
package search
