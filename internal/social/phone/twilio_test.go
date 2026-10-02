package phone_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/social/phone"
)

// fakeTwilio answers Verify the way Twilio does, for the code it was told.
func fakeTwilio(t *testing.T, status int, body string) (phone.TwilioVerifier, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		seen = *r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return phone.TwilioVerifier{AccountSID: "AC123", AuthToken: "secret", ServiceSID: "VA456", BaseURL: srv.URL, Client: srv.Client()}, &seen
}

func TestTwilioStartAndCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	v, seen := fakeTwilio(t, http.StatusCreated, `{"status":"pending"}`)
	if err := v.Start(ctx, "+351912345678"); err != nil {
		t.Fatal(err)
	}
	user, pass, ok := seen.BasicAuth()
	if seen.URL.Path != "/Services/VA456/Verifications" || !ok || user != "AC123" || pass != "secret" {
		t.Fatalf("request = %s, auth %q/%q", seen.URL.Path, user, pass)
	}
	if seen.PostForm.Get("To") != "+351912345678" || seen.PostForm.Get("Channel") != "sms" {
		t.Fatalf("form = %v", seen.PostForm)
	}

	v, seen = fakeTwilio(t, http.StatusOK, `{"status":"approved"}`)
	if ok, err := v.Check(ctx, "+351912345678", "123456"); err != nil || !ok {
		t.Fatalf("approved = %v, %v", ok, err)
	}
	if seen.URL.Path != "/Services/VA456/VerificationCheck" || seen.PostForm.Get("Code") != "123456" {
		t.Fatalf("check request = %s %v", seen.URL.Path, seen.PostForm)
	}

	v, _ = fakeTwilio(t, http.StatusOK, `{"status":"pending"}`)
	if ok, err := v.Check(ctx, "+351912345678", "000001"); err != nil || ok {
		t.Fatalf("wrong code = %v, %v; want false, nil", ok, err)
	}
}

func TestTwilioErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusBadRequest, `{"code":60200,"message":"Invalid parameter To: +351912345678"}`, phone.ErrUndeliverable},
		{http.StatusBadRequest, `{"code":60205}`, phone.ErrUndeliverable},
		{http.StatusTooManyRequests, `{"code":60203}`, phone.ErrTooManyAttempts},
		{http.StatusNotFound, `{"code":20404}`, phone.ErrExpired},
	} {
		v, _ := fakeTwilio(t, tc.status, tc.body)
		if err := v.Start(context.Background(), "+351912345678"); !errors.Is(err, tc.want) {
			t.Errorf("%d %s: %v, want %v", tc.status, tc.body, err, tc.want)
		}
	}

	// Anything else is a failure that says what happened and never echoes the
	// body, which can hold the number.
	v, _ := fakeTwilio(t, http.StatusInternalServerError, `{"code":20500,"message":"+351912345678 broke"}`)
	err := v.Start(context.Background(), "+351912345678")
	if err == nil || strings.Contains(err.Error(), "351912345678") {
		t.Fatalf("err = %v", err)
	}
}

func TestDevVerifierAcceptsOnlyItsCode(t *testing.T) {
	t.Parallel()
	v := phone.DevVerifier{}
	if ok, _ := v.Check(context.Background(), "+351912345678", phone.DevCode); !ok {
		t.Fatal("dev code refused")
	}
	if ok, _ := v.Check(context.Background(), "+351912345678", "123456"); ok {
		t.Fatal("another code accepted")
	}
}
