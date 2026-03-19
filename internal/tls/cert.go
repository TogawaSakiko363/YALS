package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

// GenerateSelfSignedCert generates a self-signed certificate with the given SNI
// Only generates if certificates don't exist or are invalid
func GenerateSelfSignedCert(certFile, keyFile, sni string) error {
	// Check if files already exist and are valid
	if isValidCertKeyPair(certFile, keyFile) {
		// Valid certificate exists, use it (whether user-provided or temporary)
		return nil
	}

	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   sni,
			Organization: []string{"YALS Temporary Certificate"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().AddDate(1, 0, 0), // Valid for 1 year
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames: []string{sni, "localhost", "127.0.0.1"},
	}

	// Create certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate to file
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		return fmt.Errorf("failed to write certificate file: %w", err)
	}

	// Write private key to file
	keyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyBytes,
	})

	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	return nil
}

// isValidCertKeyPair checks if both cert and key files exist and are readable
func isValidCertKeyPair(certFile, keyFile string) bool {
	// Check if both files exist
	certInfo, err := os.Stat(certFile)
	if err != nil || certInfo.IsDir() {
		return false
	}

	keyInfo, err := os.Stat(keyFile)
	if err != nil || keyInfo.IsDir() {
		return false
	}

	// Try to read and parse the certificate
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return false
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return false
	}

	_, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	// Try to read and parse the key
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return false
	}

	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return false
	}

	_, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return false
	}

	return true
}
