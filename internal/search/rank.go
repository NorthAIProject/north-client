package search

// The canonical full-text expressions.
//
// These are constants and not string-building helpers because the queries that
// use them live in .sql files read by sqlc, which cannot interpolate Go. So the
// real guarantee is not that callers share code — it is that every .sql file
// spells these identically, which rank_test.go asserts by reading them.
//
// It matters more than style. The indexes created in
// migrations/20260808090000_add_memory_and_message_search.sql are expression
// indexes: a query that says 'simple' where the index says 'english', or omits
// the config argument entirely, is not a slower query — it is a sequential scan
// over every row the user owns, and nothing will report that it happened.
const (
	// Config is the text search configuration. Changing it invalidates every
	// expression index in the migrations.
	Config = "english"

	// VectorExpr must match the indexed expression exactly.
	VectorExpr = `to_tsvector('english', content)`

	// QueryExpr is how user input enters a query.
	//
	// websearch_to_tsquery is the only tsquery function safe to hand arbitrary
	// human text (see Normalise), but it joins terms with AND — right for a
	// search box, wrong for North, where the "query" is a sentence somebody
	// said out loud. "my shoulder hurts when I press overhead" ANDed matches no
	// stored fact at all. Replacing the operator on the *parsed* query asks
	// what was meant instead: which facts touch any of this, best first.
	//
	// Safe because the replace happens after parsing: the text form of a
	// tsquery is quoted lexemes joined by operators, so '&' cannot occur inside
	// a lexeme. Phrase (<->) and negation (!) operators survive untouched.
	QueryExpr = `replace(websearch_to_tsquery('english', @query::text)::text, '&', '|')::tsquery`

	// RankExpr scores a row against the query.
	//
	// The 32 is a normalisation flag meaning rank/(rank+1), which squashes the
	// score into 0..1. Raw ts_rank_cd values are unbounded and depend on
	// document length, so scores from two different tables cannot be compared —
	// and comparing them is exactly what merging memories with document chunks
	// into one ranked list requires.
	RankExpr = `ts_rank_cd(to_tsvector('english', content), ` + QueryExpr + `, 32)`

	// HeadlineOpts are the ts_headline settings that produce a snippet.
	//
	// The bracket markers are borrowed from SQLite's FTS5 snippet() default and
	// kept deliberately plain: the snippet is shown to a model as often as to a
	// person, and HTML tags in a prompt are noise the model has to ignore.
	HeadlineOpts = `StartSel=[, StopSel=], MaxWords=35, MinWords=15, ShortWord=3, HighlightAll=FALSE`
)
