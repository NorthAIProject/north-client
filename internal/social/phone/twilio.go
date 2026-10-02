package phone

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const (
	twilioVerifyBaseURL = "https://verify.twilio.com/v2"
	twilioTimeout       = 15 * time.Second
)

// TwilioVerifier sends codes with Twilio Verify (v2): a Verify service holds
// the code, its expiry and its attempt count, so nothing secret is stored
// here. Plain net/http rather than Twilio's SDK: two form posts do not earn a
// dependency.
type TwilioVerifier struct {
	AccountSID string
	AuthToken  string
	ServiceSID string

	// BaseURL and Client are for tests; zero values talk to Twilio.
	BaseURL string
	Client  *http.Client
}

// Start posts a Verification with Channel=sms.
func (t TwilioVerifier) Start(ctx context.Context, number string) error {
	status, body, err := t.post(ctx, "Verifications", url.Values{"To": {number}, "Channel": {"sms"}})
	if err != nil {
		return err
	}
	if status == http.StatusOK || status == http.StatusCreated {
		return nil
	}
	return twilioError(status, body, "start verification")
}

// Check posts a VerificationCheck; approved means the code was right.
func (t TwilioVerifier) Check(ctx context.Context, number, code string) (bool, error) {
	status, body, err := t.post(ctx, "VerificationCheck", url.Values{"To": {number}, "Code": {code}})
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, twilioError(status, body, "check verification")
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, apperr.Wrap(err, "twilio: decode verification check")
	}
	return out.Status == "approved", nil
}

func (t TwilioVerifier) post(ctx context.Context, resource string, form url.Values) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, twilioTimeout)
	defer cancel()
	base := t.BaseURL
	if base == "" {
		base = twilioVerifyBaseURL
	}
	endpoint := strings.TrimRight(base, "/") + "/Services/" + url.PathEscape(t.ServiceSID) + "/" + resource
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, apperr.Wrap(err, "twilio: build request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, apperr.Wrap(unwrapURL(err), "twilio: call %s", resource)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return 0, nil, apperr.Wrap(err, "twilio: read %s", resource)
	}
	return resp.StatusCode, body, nil
}

// Twilio error codes this package tells apart. Everything else is a plain
// failure with its status and code, and never the response body, which can
// echo the number back.
const (
	twilioInvalidParameter = 60200
	twilioMaxCheckAttempts = 60202
	twilioMaxSendAttempts  = 60203
	twilioLandline         = 60205
	twilioBlockedPrefix    = 60410
	twilioNotFound         = 20404
)

func twilioError(status int, body []byte, what string) error {
	var e struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(body, &e)
	switch {
	case e.Code == twilioMaxCheckAttempts, e.Code == twilioMaxSendAttempts, status == http.StatusTooManyRequests:
		return ErrTooManyAttempts
	case e.Code == twilioInvalidParameter, e.Code == twilioLandline, e.Code == twilioBlockedPrefix:
		return ErrUndeliverable
	case e.Code == twilioNotFound, status == http.StatusNotFound:
		return ErrExpired
	}
	return apperr.Wrap(apperr.ErrUnavailable, "twilio: %s failed with status %d, code %d", what, status, e.Code)
}

// unwrapURL drops the *url.Error wrapper, whose message repeats the request
// URL. Harmless here, but the habit keeps credentials out of logs elsewhere.
func unwrapURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
