package apns

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testKeyPEM is a fresh P-256 key in the PKCS#8 PEM form Apple's .p8 uses.
func testKeyPEM(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), key
}

type seen struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   []string
}

// fakeApple answers with each reply in turn, then 200 once they run out.
func fakeApple(t *testing.T, replies ...func(w http.ResponseWriter)) (*httptest.Server, *seen) {
	t.Helper()
	got := &seen{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.mu.Lock()
		defer got.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		got.requests = append(got.requests, r)
		got.bodies = append(got.bodies, string(body))
		if n := len(got.requests); n <= len(replies) {
			replies[n-1](w)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, got
}

func senderFor(t *testing.T, srv *httptest.Server) (*HTTPSender, *ecdsa.PrivateKey) {
	t.Helper()
	pemKey, key := testKeyPEM(t)
	s, err := NewHTTPSender("KEY123", "TEAM456", pemKey)
	if err != nil {
		t.Fatal(err)
	}
	s.client = srv.Client()
	s.hosts = map[string]string{EnvironmentProduction: srv.URL, EnvironmentSandbox: srv.URL}
	return s, key
}

var device = Device{Token: strings.Repeat("ab", 32), Topic: "com.example.app", Environment: EnvironmentProduction}

func TestSendSpeaksApplesProtocol(t *testing.T) {
	srv, got := fakeApple(t)
	s, key := senderFor(t, srv)

	res, err := s.Send(t.Context(), device, []byte(`{"aps":{}}`))
	if err != nil || !res.OK() {
		t.Fatalf("send = %+v, %v", res, err)
	}

	r := got.requests[0]
	if r.ProtoMajor != 2 {
		t.Errorf("proto = %s, want HTTP/2", r.Proto)
	}
	if r.URL.Path != "/3/device/"+device.Token {
		t.Errorf("path = %s", r.URL.Path)
	}
	for header, want := range map[string]string{
		"apns-topic":     "com.example.app",
		"apns-push-type": "alert",
		"apns-priority":  "10",
	} {
		if v := r.Header.Get(header); v != want {
			t.Errorf("%s = %q, want %q", header, v, want)
		}
	}
	if r.Header.Get("apns-expiration") == "" {
		t.Error("apns-expiration missing")
	}

	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "bearer ")
	tok, err := jwt.Parse(bearer, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{"ES256"}))
	if err != nil {
		t.Fatalf("provider token does not verify: %v", err)
	}
	if tok.Header["kid"] != "KEY123" {
		t.Errorf("kid = %v", tok.Header["kid"])
	}
	if iss, _ := tok.Claims.GetIssuer(); iss != "TEAM456" {
		t.Errorf("iss = %q", iss)
	}
}

func TestProviderTokenIsReusedUntilItAges(t *testing.T) {
	srv, got := fakeApple(t)
	s, _ := senderFor(t, srv)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }

	for range 2 {
		if _, err := s.Send(t.Context(), device, []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(providerTokenLifetime)
	if _, err := s.Send(t.Context(), device, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}

	auth := func(i int) string { return got.requests[i].Header.Get("Authorization") }
	if auth(0) != auth(1) {
		t.Error("a fresh token was signed for every send; Apple throttles that")
	}
	if auth(1) == auth(2) {
		t.Error("a token past its lifetime was reused; Apple refuses those")
	}
}

func TestAnExpiredProviderTokenIsRefreshedOnce(t *testing.T) {
	expired := func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"reason":"ExpiredProviderToken"}`)
	}
	srv, got := fakeApple(t, expired)
	s, _ := senderFor(t, srv)

	res, err := s.Send(t.Context(), device, []byte(`{}`))
	if err != nil || !res.OK() {
		t.Fatalf("send = %+v, %v; want the retry to succeed", res, err)
	}
	if len(got.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(got.requests))
	}
	if got.requests[0].Header.Get("Authorization") == got.requests[1].Header.Get("Authorization") {
		t.Error("the retry reused the token Apple had refused")
	}
}

func TestResultGone(t *testing.T) {
	for _, tc := range []struct {
		res  Result
		gone bool
	}{
		{Result{Status: 410, Reason: "Unregistered"}, true},
		{Result{Status: 400, Reason: "BadDeviceToken"}, true},
		{Result{Status: 400, Reason: "DeviceTokenNotForTopic"}, true},
		{Result{Status: 403, Reason: "InvalidProviderToken"}, false},
		{Result{Status: 429, Reason: "TooManyRequests"}, false},
		{Result{Status: 500}, false},
	} {
		if got := tc.res.Gone(); got != tc.gone {
			t.Errorf("%+v Gone = %v, want %v", tc.res, got, tc.gone)
		}
	}
}
