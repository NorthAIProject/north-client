package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntInRange(t *testing.T) {
	assert.True(t, IntInRange(1, 1, 1))
	assert.True(t, IntInRange(2, 1, 2))
	assert.False(t, IntInRange(0, 1, 2))
	assert.False(t, IntInRange(-1, 1, 2))
	assert.False(t, IntInRange(3, 1, 2))
}
