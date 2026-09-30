package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateInt(t *testing.T) {
	a := assert.New(t)

	v := GenerateInt(10)

	a.True(v <= 10)
	a.True(v >= 0)
}
