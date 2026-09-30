package util

import (
	"fmt"

	"gitlab.com/sqills/development/dev-q/s3p-travel-pass/internal/pkg/errors"
)

func FormatNote(msg string, err error) string {
	if err != nil {
		code := errors.CodeInternalServerError.Error()
		interr, ok := err.(*errors.InternalError)
		if ok {
			code = interr.ErrorCode()
		}
		return fmt.Sprintf("%s - %s", msg, code)
	}
	return msg
}
