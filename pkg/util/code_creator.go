package util

import (
	"fmt"
	"regexp"

	"github.com/lucasjones/reggen"
	"gitlab.com/sqills/development/dev-q/s3p-travel-pass/internal/pkg/errors"
)

const DefaultPattern = "[A-Z0-9]{10}"

type CodeCreator interface {
	Create(pattern string) (string, error)
	Validate(pattern, code string) error
}

type codeCreator struct {
}

func NewCodeCreator() CodeCreator {
	return &codeCreator{}
}

func (c *codeCreator) Create(pattern string) (string, error) {
	p := pattern
	if p == "" {
		p = DefaultPattern
	}

	// the limit = 0 is used only when the regex contains a '+' in the regular expression.
	code, err := reggen.Generate(p, 10)
	if err != nil {
		return "", errors.NewInternalErrorWithCause("failed to generate code by pattern", err)
	}

	return code, nil
}

func (c *codeCreator) Validate(pattern, code string) error {
	if pattern == "" { // This should never happen
		return errors.NewInternalError(fmt.Sprintf("no pattern provided: %v", pattern))
	}
	ok, err := regexp.MatchString(pattern, code)
	if err != nil {
		return err
	}
	if !ok {
		return errors.NewWithCause(fmt.Sprintf("provided code does not match the configured pattern: %v", pattern), errors.CodeInstanceCodeDoesNotRespectPattern, err)
	}

	return nil
}
