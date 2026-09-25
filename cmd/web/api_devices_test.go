package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

func apnsKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestDevicesAPIRegistersAndForgetsAToken(t *testing.T) {
	keyPEM := apnsKeyPEM(t)
	handler, pool := testRoutesAndPool(t, func(cfg *config.Config) {
		cfg.APNs = config.APNsConfig{
			KeyID: "KEY123", TeamID: "TEAM456", PrivateKey: keyPEM,
			Topics: []string{"com.fernandocorreia.khepri", "com.fernandocorreia.khepri.beta"},
		}
	})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}
	tok := strings.Repeat("ab", 32)

	rec := api.call(http.MethodPut, "/api/v1/devices/apns",
		`{"token":"`+tok+`","topic":"com.fernandocorreia.khepri.beta","environment":"production"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}

	rec = api.call(http.MethodPut, "/api/v1/devices/apns",
		`{"token":"`+tok+`","topic":"com.someone.else","environment":"production"}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "topic") {
		t.Fatalf("foreign topic: %d %s, want 422 on topic", rec.Code, rec.Body)
	}

	if rec = api.call(http.MethodDelete, "/api/v1/devices/apns/"+tok, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("unregister: %d %s", rec.Code, rec.Body)
	}
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM apns_devices`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("devices left = %d, %v", n, err)
	}
}

// Without a key the server cannot send, so it does not keep tokens either.
func TestDevicesAPIIsUnavailableWithoutAKey(t *testing.T) {
	api := newAPIClient(t)
	rec := api.call(http.MethodPut, "/api/v1/devices/apns",
		`{"token":"`+strings.Repeat("ab", 32)+`","topic":"com.fernandocorreia.khepri","environment":"production"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d %s, want 503", rec.Code, rec.Body)
	}
}
