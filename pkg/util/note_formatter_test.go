package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gitlab.com/sqills/development/dev-q/s3p-travel-pass/internal/pkg/errors"
)

func Test_FormatNote_WithError(t *testing.T) {
	msg := "some message"
	err := errors.New("some_error", errors.CodeInvalidRequest)

	res := FormatNote(msg, err)

	assert.Equal(t, "some message - invalid_request", res)
}

func Test_FormatNote_NoError(t *testing.T) {
	msg := "some message"

	res := FormatNote(msg, nil)

	assert.Equal(t, "some message", res)
}
