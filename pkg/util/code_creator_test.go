package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCodeCreator_Create(t *testing.T) {
	c := NewCodeCreator()

	_, err := c.Create("[0-9")
	assert.Error(t, err)

	v1, _ := c.Create("")
	v2, _ := c.Create("")
	v3, _ := c.Create("[A-Z]{50}")

	assert.NotNil(t, v1)
	assert.NotNil(t, v2)
	assert.NotNil(t, v3)
	assert.NotEqual(t, v1, v2)
	assert.NotEqual(t, v2, v3)
	assert.Len(t, v1, 10)
	assert.Len(t, v2, 10)
	assert.Len(t, v3, 50)
}

func TestCodeCreator_Validate(t *testing.T) {
	c := NewCodeCreator()

	assert.NoError(t, c.Validate("[A-Z0-9]{6}", "SOMECODE"))
	assert.Error(t, c.Validate("", "No pattern? Anything goes, right? Wrong."))
	assert.Error(t, c.Validate("[A-Z0-9]{6}", ""))
	assert.Error(t, c.Validate("[A-Z0-9]{6}", " "))
	assert.Error(t, c.Validate("something", "ABC123"))
}
