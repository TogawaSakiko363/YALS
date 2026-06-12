// Package tls provides the YALS built-in TLS certificate. The server and agent
// embed the SAME fixed self-signed certificate: the server serves it and the
// agent pins it to verify the server. No per-deployment fingerprint or CA is
// needed, and custom certificates are not supported.
//
// SECURITY NOTE: the private key below is intentionally shipped with the
// software. It provides transport encryption and resistance to a casual
// man-in-the-middle, but NOT against an attacker who possesses the binary (they
// can extract the key and impersonate the server). Agent identity remains
// protected by the per-agent token, which is the real secret.
package tls

import (
	_ "embed"
	"encoding/pem"
	"fmt"
)

//go:embed builtin_cert.pem
var builtinCertPEM []byte

//go:embed builtin_key.pem
var builtinKeyPEM []byte

// BuiltinCertPEM returns the embedded server certificate in PEM form.
func BuiltinCertPEM() []byte { return builtinCertPEM }

// BuiltinKeyPEM returns the embedded server private key in PEM form.
func BuiltinKeyPEM() []byte { return builtinKeyPEM }

// BuiltinCertDER returns the DER bytes of the embedded leaf certificate. The
// agent compares the certificate the server presents against exactly these bytes.
func BuiltinCertDER() ([]byte, error) {
	block, _ := pem.Decode(builtinCertPEM)
	if block == nil {
		return nil, fmt.Errorf("builtin certificate: no PEM block")
	}
	return block.Bytes, nil
}
