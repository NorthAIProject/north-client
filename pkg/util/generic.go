package util

import (
	"sync"

	"golang.org/x/sync/errgroup"
)

func ForEachGo[T any](l []T, block func(T)) {
	var wg sync.WaitGroup
	wg.Add(len(l))
	for _, code := range l {
		go func(code T) {
			defer wg.Done()
			block(code)
		}(code)
	}
	wg.Wait()
}

func ForEachGoErr[T any](l []T, block func(T) error) error {
	errg := new(errgroup.Group)
	for _, code := range l {
		errg.Go(func() error {
			err := block(code)
			return err
		})
	}
	return errg.Wait()
}

func Any[T any](l []T, eval func(T) bool) bool {
	for _, i := range l {
		if eval(i) {
			return true
		}
	}
	return false
}

func Filter[T any](l []T, eval func(T) bool) []T {
	out := make([]T, 0)
	for _, i := range l {
		if eval(i) {
			out = append(out, i)
		}
	}
	return out
}

func Find[T any](l []T, eval func(T) bool) (out T) {
	for _, i := range l {
		if eval(i) {
			out = i
			break
		}
	}
	return out
}

func FindOk[T comparable](l []T, eval func(T) bool) (T, bool) {
	out := Find(l, eval)
	var x T
	return out, x != out
}

func Map[F, T any](l []F, eval func(F) T) []T {
	out := make([]T, len(l))
	for index, i := range l {
		out[index] = eval(i)
	}
	return out
}

func MapErr[F, T any](l []F, eval func(F) (T, error)) ([]T, error) {
	out := make([]T, len(l))
	for index, i := range l {
		val, err := eval(i)
		if err != nil {
			return nil, err
		}
		out[index] = val
	}
	return out, nil
}

func MapGoErr[I, O any](input []I, eval func(I) (O, error)) ([]O, error) {
	output := make([]O, len(input))
	errg := new(errgroup.Group)
	for index, i := range input {
		errg.Go(func() error {
			val, err := eval(i)
			if err != nil {
				return err
			}
			output[index] = val
			return nil
		})
	}
	if err := errg.Wait(); err != nil {
		return nil, err
	}
	return output, nil
}

func ReduceErr[T any, R any](collection []T, accumulator func(agg R, item T) (R, error), initial R) (R, error) {
	var err error
	for i := range collection {
		initial, err = accumulator(initial, collection[i])
		if err != nil {
			var empty R
			return empty, err
		}
	}
	return initial, nil
}

func CopySlice[T any](original []T) []T {
	target := make([]T, len(original))
	copy(target, original)
	return target
}

func Chunk[T any](l []T, size int) [][]T {
	var chunked [][]T
	for i := 0; i < len(l); i += size {
		end := i + size
		if end > len(l) {
			end = len(l)
		}
		chunked = append(chunked, l[i:end])
	}
	return chunked
}

func OrDefault[T comparable](x T, fallback T) T {
	var zero T
	if x == zero {
		return fallback
	}
	return x
}
