package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// classify puts an API failure in the class the runner acts on. The SDK's
// message is kept, the request is not: it carries the key in a header.
func classify(err error) error {
	var apiErr *sdk.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("anthropic: %w", err)
	}

	detail := apiMessage(apiErr)
	if detail == "" {
		detail = http.StatusText(apiErr.StatusCode)
	}
	status := apiErr.StatusCode

	switch {
	// Anthropic reports an empty balance as a 400, not a 402. Waiting will not
	// fix it and it says nothing about the key, so it is billing.
	case status == http.StatusPaymentRequired, strings.Contains(strings.ToLower(detail), "credit balance"):
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrPaymentRequired, status, detail)
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrForbidden, status, detail)
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrUnavailable, status, detail)
	default:
		return fmt.Errorf("anthropic returned %d: %s", status, detail)
	}
}

// stopError turns a stop reason that carries no answer into an error. A
// refusal is a declined request, not a reply: posting it would be an empty
// message, and the next provider in the chain may well answer.
func stopError(reason sdk.StopReason) error {
	if reason == sdk.StopReasonRefusal {
		return fmt.Errorf("%w: anthropic declined the request", apperr.ErrUnavailable)
	}
	return nil
}

// apiMessage reads the error message out of the response body. The SDK's own
// Error() string also carries the request line, which is noise in a log and
// in a settings page.
func apiMessage(apiErr *sdk.Error) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(apiErr.RawJSON()), &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Error.Message)
}
