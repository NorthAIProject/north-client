package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// classify puts an API failure in the class the runner acts on, by this
// client's rules.
func (c *Client) classify(err error) error {
	return classify(err, c.managed)
}

// classify puts an API failure in the class the runner acts on. The SDK's
// message is kept, the request is not: it carries the key in a header.
//
// A managed client sends any other client error to the next provider too.
// Its chain predates it and answers the same requests, so a 400 or a 404
// here (an effort the model does not take, a retired model name) is this
// provider's problem, not the caller's.
//
// A failure that never got an API answer (DNS, a refused or reset
// connection, a stream cut short) is unavailability: it says nothing about
// the request, and the next provider may well reach its own service. The
// caller cancelling, or its deadline passing, is the exception, and is left
// unmarked so the runner stops rather than spending the chain on nobody.
func classify(err error, managed bool) error {
	var apiErr *sdk.Error
	if !errors.As(err, &apiErr) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("anthropic: %w", err)
		}
		return fmt.Errorf("%w: anthropic: %w", apperr.ErrUnavailable, err)
	}

	detail := apiMessage(apiErr)
	if detail == "" {
		detail = http.StatusText(apiErr.StatusCode)
	}
	status := apiErr.StatusCode

	lower := strings.ToLower(detail)

	switch {
	// Anthropic reports an empty balance, and a workspace that has hit its
	// spend limit, as a 400 rather than a 402. Waiting will not fix either and
	// neither says anything about the key, so both are billing.
	case status == http.StatusPaymentRequired,
		strings.Contains(lower, "credit balance"),
		strings.Contains(lower, "usage limit"):
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrPaymentRequired, status, detail)
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrForbidden, status, detail)
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrUnavailable, status, detail)
	// An error event inside a stream arrives with the stream's own status,
	// a 200, so only its type says what went wrong.
	case transientType(apiErrorType(apiErr)):
		return fmt.Errorf("%w: anthropic returned %s: %s", apperr.ErrUnavailable, apiErrorType(apiErr), detail)
	case managed && status >= 400:
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrUnavailable, status, detail)
	default:
		return fmt.Errorf("anthropic returned %d: %s", status, detail)
	}
}

// stopError turns a stop reason that carries no answer into an error. A
// refusal is a declined request, not a reply: posting it would be an empty
// message, and the next provider in the chain may well answer. So is running
// out of tokens before writing anything, which thinking makes possible: the
// whole budget can go on reasoning that is never shown.
func stopError(reason sdk.StopReason, content []sdk.ContentBlockUnion) error {
	switch {
	case reason == sdk.StopReasonRefusal:
		return fmt.Errorf("%w: anthropic declined the request", apperr.ErrUnavailable)
	case reason == sdk.StopReasonMaxTokens && !answered(content):
		return fmt.Errorf("%w: anthropic reached max_tokens before answering", apperr.ErrUnavailable)
	}
	return nil
}

// answered reports whether a reply holds anything a caller can use: text, or
// a tool call.
func answered(content []sdk.ContentBlockUnion) bool {
	for _, block := range content {
		switch b := block.AsAny().(type) {
		case sdk.TextBlock:
			if strings.TrimSpace(b.Text) != "" {
				return true
			}
		case sdk.ToolUseBlock:
			return true
		}
	}
	return false
}

// transientType reports whether an API error type means the service, not the
// request, is the problem.
func transientType(kind string) bool {
	switch kind {
	case "overloaded_error", "api_error", "rate_limit_error":
		return true
	}
	return false
}

// apiErrorType reads the error type ("overloaded_error", ...) out of the body.
func apiErrorType(apiErr *sdk.Error) string {
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(apiErr.RawJSON()), &body); err != nil {
		return ""
	}
	return body.Error.Type
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
