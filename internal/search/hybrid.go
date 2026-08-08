package search

// Ranked is anything retrieval can return that has an identity and a position.
//
// Generic over the payload so this file knows nothing about documents, chunks,
// or memories — fusing two ranked lists is arithmetic, and arithmetic that
// imports a domain type is arithmetic nobody can reuse.
type Ranked interface {
	// Key identifies the item across lists. Two entries with one key are the
	// same thing found twice, which is exactly the case fusion exists for.
	Key() string
}

// rrfK damps the contribution of low-ranked results.
//
// 60 is the value from the paper that introduced reciprocal rank fusion
// (Cormack et al.), and it has held up as a default well enough that changing
// it should be driven by a measurement rather than a preference. Its effect:
// the gap between rank 1 and rank 2 matters far more than the gap between rank
// 30 and 31, which is how people actually read results.
const rrfK = 60.0

// Fuse merges ranked lists into one, by reciprocal rank.
//
// # Why not just add the scores
//
// The two retrievers do not share a scale and cannot be made to. ts_rank_cd is
// a normalised term-frequency score; cosine similarity is an angle between
// vectors. A passage scoring 0.4 on one is not comparable to 0.4 on the other,
// and a weighted sum quietly encodes a claim about their relative meaning that
// nobody has evidence for.
//
// Reciprocal rank fusion ignores the scores entirely and uses only the
// positions, which is the one thing both lists genuinely agree on: this was my
// best answer, this was my second. It needs no calibration, no tuning per
// corpus, and no retraining when a model changes.
//
// An item found by both retrievers accumulates from both, which is the
// behaviour worth having: agreement between two methods that fail differently
// is the strongest signal either can give.
func Fuse[T Ranked](lists ...[]T) []T {
	scores := make(map[string]float64)
	first := make(map[string]T)
	order := make([]string, 0)

	for _, list := range lists {
		for position, item := range list {
			key := item.Key()
			if _, seen := scores[key]; !seen {
				first[key] = item
				order = append(order, key)
			}
			scores[key] += 1.0 / (rrfK + float64(position+1))
		}
	}

	// Sorted by fused score, and ties broken by the order first encountered so
	// the result is deterministic. A retrieval that returns a different order
	// for the same inputs makes every downstream test flaky and every bug
	// report unreproducible.
	position := make(map[string]int, len(order))
	for i, key := range order {
		position[key] = i
	}

	out := make([]T, 0, len(order))
	for _, key := range order {
		out = append(out, first[key])
	}

	sortStable(out, func(a, b T) bool {
		ka, kb := a.Key(), b.Key()
		if scores[ka] != scores[kb] {
			return scores[ka] > scores[kb]
		}
		return position[ka] < position[kb]
	})

	return out
}

// sortStable is insertion sort over a small slice.
//
// Retrieval lists here are tens of items, and this avoids importing sort just
// to pass a closure that captures two maps.
func sortStable[T any](items []T, less func(a, b T) bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
