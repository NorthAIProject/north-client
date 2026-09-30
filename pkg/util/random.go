package util

import (
	"math/rand"
)

func GenerateInt(max int) int {
	return rand.Intn(max)
}
