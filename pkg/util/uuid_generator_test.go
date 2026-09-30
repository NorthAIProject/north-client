package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUUIDGenerator(t *testing.T) {
	assert.NotEmpty(t, NewUUIDGenerator().Generate())
}
