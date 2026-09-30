package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewBackoff(t *testing.T) {
	t.Run("BackoffStrategyStatic", func(t *testing.T) {
		backoff := NewBackoff(BackoffStrategyStatic(time.Minute))
		assert.Equal(t, time.Minute, backoff(1))
	})
	t.Run("BackoffStrategyExponential", func(t *testing.T) {
		backoff := NewBackoff(BackoffStrategyExponential(time.Minute, 2))
		assert.Equal(t, time.Minute*1, backoff(0))
		assert.Equal(t, time.Minute*4, backoff(2))
		assert.Equal(t, time.Minute*32, backoff(5))
	})
	t.Run("BackoffWithMax should limit to max", func(t *testing.T) {
		backoff := NewBackoff(BackoffWithMax(time.Second*30), BackoffStrategyStatic(time.Second*10))
		assert.Equal(t, time.Second*10, backoff(1))
	})
	t.Run("BackoffWithMax should not limit to max", func(t *testing.T) {
		backoff := NewBackoff(BackoffWithMax(time.Second*30), BackoffStrategyStatic(time.Minute))
		assert.Equal(t, time.Second*30, backoff(1))
	})
}
