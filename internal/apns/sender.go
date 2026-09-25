package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Sender delivers one payload to one device and reports Apple's answer.
// The Service turns that answer into bookkeeping; a Sender only talks HTTP.
type Sender interface {
	Send(ctx context.Context, d Device, payload []byte) (Result, error)
}

// Result is Apple's answer to one send: the HTTP status and, on a refusal,
// the reason string from the body ("BadDeviceToken", "Unregistered", ...).
type Result struct {
	Status int
	Reason string
}

// Gone reports whether Apple has said this token will never work again, so
// the row should be deleted rather than retried.
//
// BadDeviceToken from the production host usually means a sandbox token, a
// build configuration mistake, not a lost device. Deleting is still right: the
// app registers again on every launch, and a wrong row only fails forever.
func (r Result) Gone() bool {
	if r.Status == http.StatusGone {
		return true
	}
	switch r.Reason {
	case "BadDeviceToken", "Unregistered", "DeviceTokenNotForTopic", "ExpiredToken":
		return true
	}
	return false
}

func (r Result) OK() bool { return r.Status >= 200 && r.Status < 300 }

// Hosts Apple serves each environment from. Tests point them at httptest.
var hosts = map[string]string{
	EnvironmentProduction: "https://api.push.apple.com",
	EnvironmentSandbox:    "https://api.sandbox.push.apple.com",
}

// providerTokenLifetime is how long a signed token is reused. Apple refuses
// one older than an hour and throttles one refreshed more than every twenty
// minutes, so fifty minutes sits inside both.
const providerTokenLifetime = 50 * time.Minute

// HTTPSender speaks Apple's HTTP/2 provider API with token authentication.
type HTTPSender struct {
	keyID  string
	teamID string
	key    *ecdsa.PrivateKey
	client *http.Client
	hosts  map[string]string
	now    func() time.Time

	mu       sync.Mutex
	token    string
	signedAt time.Time
}

// NewHTTPSender parses the .p8 key. The default transport negotiates HTTP/2
// over TLS on its own, which is all Apple asks for.
func NewHTTPSender(keyID, teamID, privateKeyPEM string) (*HTTPSender, error) {
	key, err := jwt.ParseECPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return nil, apperr.Wrap(err, "parse APNS_PRIVATE_KEY")
	}
	return &HTTPSender{
		keyID:  keyID,
		teamID: teamID,
		key:    key,
		client: &http.Client{Timeout: 10 * time.Second},
		hosts:  hosts,
		now:    time.Now,
	}, nil
}

// Send posts the payload, and retries once with a fresh provider token if
// Apple says the cached one has expired or is invalid.
func (s *HTTPSender) Send(ctx context.Context, d Device, payload []byte) (Result, error) {
	res, err := s.post(ctx, d, payload, false)
	if err == nil && (res.Reason == "ExpiredProviderToken" || res.Reason == "InvalidProviderToken") {
		return s.post(ctx, d, payload, true)
	}
	return res, err
}

func (s *HTTPSender) post(ctx context.Context, d Device, payload []byte, refresh bool) (Result, error) {
	host, ok := s.hosts[d.Environment]
	if !ok {
		return Result{}, fmt.Errorf("apns: unknown environment %q", d.Environment)
	}
	bearer, err := s.providerToken(refresh)
	if err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/3/device/"+d.Token, bytes.NewReader(payload))
	if err != nil {
		return Result{}, apperr.Wrap(err, "build apns request")
	}
	req.Header.Set("Authorization", "bearer "+bearer)
	req.Header.Set("apns-topic", d.Topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	// A nudge a day late is noise, so Apple may drop it after a day offline.
	req.Header.Set("apns-expiration", strconv.FormatInt(s.now().Add(24*time.Hour).Unix(), 10))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return Result{}, apperr.Wrap(err, "send to apns")
	}
	defer func() { _ = resp.Body.Close() }()

	res := Result{Status: resp.StatusCode}
	if !res.OK() {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		res.Reason = body.Reason
	}
	return res, nil
}

// providerToken returns the cached signed token, signing a new one when it is
// older than providerTokenLifetime or the caller asks for a refresh.
func (s *HTTPSender) providerToken(refresh bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if !refresh && s.token != "" && now.Sub(s.signedAt) < providerTokenLifetime {
		return s.token, nil
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": s.teamID,
		"iat": now.Unix(),
	})
	tok.Header["kid"] = s.keyID
	signed, err := tok.SignedString(s.key)
	if err != nil {
		return "", apperr.Wrap(err, "sign apns provider token")
	}
	s.token, s.signedAt = signed, now
	return signed, nil
}
