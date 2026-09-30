package util

import (
	"golang.org/x/exp/constraints"
)

func Ptr[T any](s T) *T {
	return &s
}

func StrZeroPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func Val[T any](s *T) T {
	var x T
	if s == nil {
		return x
	}

	return *s
}

func BoolValOrDefault(boolPtr *bool, d bool) bool {
	if boolPtr == nil {
		return d
	}
	return *boolPtr
}

func IntZeroPtr[T constraints.Integer](i T) *T {
	if i == 0 {
		return nil
	}

	return &i
}

// PtrF takes a function that returns a value of type T and returns a function that returns a pointer to T.
// This is useful for creating pointer values in combination with lo.If or similar functions.
func PtrF[T any](f func() T) func() *T {
	return func() *T {
		v := f()
		return &v
	}
}
