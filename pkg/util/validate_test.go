package util

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate_WithoutCodes(t *testing.T) {
	assert.NoError(t, Validate([]string{}, 2, func(s string) error { return nil }))
}

func TestValidate_WithoutError(t *testing.T) {
	lock := sync.RWMutex{}
	validated := map[string]bool{}
	// Testing validator that never returns errors
	validator := func(s string) error {
		lock.Lock()
		validated[s] = true
		lock.Unlock()
		return nil
	}
	codes := []string{"1", "2", "3", "4", "5"}
	err := Validate(codes, 2, validator)
	assert.NoError(t, err)
	assert.Equal(t, map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true}, validated)
}

func TestValidate_WithError(t *testing.T) {
	lock := sync.RWMutex{}
	validated := map[string]bool{}
	// Testing validator that returns errors for static values
	validator := func(s string) error {
		lock.Lock()
		validated[s] = true
		lock.Unlock()
		if s == "4" {
			return fmt.Errorf("some validation err")
		}
		return nil
	}
	codes := []string{"1", "2", "3", "4", "5"}
	err := Validate(codes, 2, validator)
	assert.Error(t, err)
	assert.Equal(t, map[string]bool{"1": true, "2": true, "3": true, "4": true}, validated)
}

func TestValidate_WithMultipleError(t *testing.T) {
	// Testing validator that returns errors for all values
	validator := func(s string) error {
		return fmt.Errorf("some validation err")
	}
	codes := []string{"1", "2", "3", "4", "5"}
	err := Validate(codes, 5, validator)
	assert.Error(t, err)
}
