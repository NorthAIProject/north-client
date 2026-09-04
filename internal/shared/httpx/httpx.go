// Package httpx is the JSON edge: reading a request body, writing a reply, and
// turning a domain error into a status code the same way everywhere.
//
// It exists because two slices had already grown their own copy and the copies
// had drifted apart. internal/auth rejected unknown fields but read an unbounded
// body; internal/push capped the body but accepted unknown fields. Neither did
// both, and neither choice was wrong for the other's caller — which is why the
// strictness is an argument here rather than a decision baked into the package.
package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ErrTooLarge is a body that exceeded ReadOptions.MaxBytes.
//
// Its own error rather than an apperr sentinel: 413 is about the transport
// rather than the request being wrong, and nothing in the domain can raise it.
var ErrTooLarge = errors.New("request body too large")

// ErrRateLimited is a caller who has spent their allowance.
//
// Its own error for the same reason: a quota refusal is about how often, not
// about the request being wrong, and internal/quota already answers 429 on the
// HTML side. The two edges have to agree.
var ErrRateLimited = errors.New("rate limited")

// ReadOptions is how strict one endpoint wants to be. The zero value reads an
// unbounded body and rejects unknown fields.
type ReadOptions struct {
	// MaxBytes caps the body. Zero means no cap.
	MaxBytes int64

	// AllowUnknownFields keeps fields the target struct does not declare.
	//
	// Wanted by anything decoding a browser-supplied shape: a PushSubscription
	// carries expirationTime today and whatever the spec adds tomorrow, and a
	// decoder that refused those would refuse every real browser.
	AllowUnknownFields bool
}

// ReadJSON decodes a JSON request body into dst.
//
// Returns ErrTooLarge, or an error wrapping apperr.ErrValidation when the body
// is not JSON of the required shape. The caller writes the reply, because the
// wording of a bad-body message belongs to the endpoint that knows what it
// wanted.
func ReadJSON(w http.ResponseWriter, r *http.Request, dst any, opts ReadOptions) error {
	defer func() { _ = r.Body.Close() }()

	if opts.MaxBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, opts.MaxBytes)
	}

	dec := json.NewDecoder(r.Body)
	if !opts.AllowUnknownFields {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return ErrTooLarge
		}
		return apperr.Wrap(errors.Join(apperr.ErrValidation, err), "decode request body")
	}
	return nil
}

// WriteJSON writes v as the whole reply.
//
// An encoding failure is swallowed on purpose: the status line has already
// gone out by then, so there is no second answer to give and the only honest
// thing left is to stop writing.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Status maps a domain error to the code that describes it.
//
// An error this does not recognise is a 500. That is the safe wrong answer:
// reporting an unknown failure as a client error tells the caller to change
// something they cannot change.
func Status(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrRateLimited):
		return http.StatusTooManyRequests
	case apperr.Is(err, apperr.ErrValidation):
		return http.StatusUnprocessableEntity
	case apperr.Is(err, apperr.ErrNotFound):
		return http.StatusNotFound
	case apperr.Is(err, apperr.ErrUnauthenticated):
		return http.StatusUnauthorized
	case apperr.Is(err, apperr.ErrForbidden):
		return http.StatusForbidden
	case apperr.Is(err, apperr.ErrConflict):
		return http.StatusConflict
	case apperr.Is(err, apperr.ErrPaymentRequired):
		return http.StatusPaymentRequired
	case apperr.Is(err, apperr.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// ErrorBody is the one error shape every JSON endpoint answers with.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries the message and, for a validation failure, which fields
// were wrong.
type ErrorDetail struct {
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Error writes err as JSON with the status Status gives it.
//
// The message is only ever the caller's own: a 500 says so in general terms
// rather than handing back a wrapped internal error, which is how a table name
// or a connection string ends up in somebody's client.
func Error(w http.ResponseWriter, err error, message string) {
	status := Status(err)

	detail := ErrorDetail{Message: message}
	var fields apperr.FieldErrors
	if apperr.As(err, &fields) {
		detail.Fields = fields.Messages()
	}

	WriteJSON(w, status, ErrorBody{Error: detail})
}
