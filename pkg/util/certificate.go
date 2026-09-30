package util

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"

	"gitlab.com/sqills/development/vectron/golang-packages/pkg/pki"
	"gitlab.com/sqills/development/vectron/golang-packages/pkg/s3passenger/common"
)

func GetCertificateAndSignature(pk, cert string, dateTime *common.DateTime) (*pki.Certificate, string, error) {
	signature, err := getSignatureByPrivateKey(pk, dateTime)
	if err != nil {
		return nil, "", fmt.Errorf("could not get signature by private key: %w", err)
	}

	pem, err := pki.PEMDecode(cert)
	if err != nil {
		return nil, "", err
	} else if pem.Certificate == nil {
		return nil, "", fmt.Errorf("pem certificate returned is nil")
	}

	return pem.Certificate, signature, nil
}

func getSignatureByPrivateKey(pk string, createdAt *common.DateTime) (string, error) {
	stripped := strings.ReplaceAll(strings.ReplaceAll(pk, "-----BEGIN PRIVATE KEY-----", ""), "-----END PRIVATE KEY-----", "")
	decoded, err := base64.StdEncoding.DecodeString(stripped)
	if err != nil {
		return "", err
	}

	res, err := parseToPrivateKey(decoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode pem: %w", err)
	}

	signature, err := createSignature(res, createdAt)
	if err != nil {
		return "", fmt.Errorf("could not create signature: %w", err)
	}

	return signature, nil
}

func parseToPrivateKey(data []byte) (*rsa.PrivateKey, error) {
	res, err := x509.ParsePKCS8PrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("could not parse data to PKCS8 private: %w", err)
	}

	pk, ok := res.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("could not parse to private key: %w", err)
	}

	return pk, nil
}

func createSignature(pk *rsa.PrivateKey, createdAt *common.DateTime) (string, error) {
	h := sha256.New()
	h.Write([]byte(createdAt.String()))
	d := h.Sum(nil)
	signature, err := rsa.SignPKCS1v15(rand.Reader, pk, crypto.SHA256, d)
	if err != nil {
		return "", fmt.Errorf("failed to sign payload: %w", err)
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}
