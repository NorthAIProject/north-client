package util

func Copy[T any](byRefItem *T, apply func(item *T)) *T {
	if byRefItem == nil {
		return nil
	}
	shallowCopy := *byRefItem
	newItem := &shallowCopy
	apply(newItem)
	return newItem
}
