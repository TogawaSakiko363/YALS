// Package tls provides the YALS built-in TLS certificate. The server serves this
// fixed self-signed certificate, and the agent trusts it out of the box (see
// internal/agent/conn.go) — so a direct agent↔server link is encrypted and
// authenticated with no configuration. The agent ADDITIONALLY accepts any
// certificate that passes standard CA validation for the server's hostname, so
// the same agent also works when the server is reached through a TLS-terminating
// reverse proxy / CDN holding a real (CA-trusted) certificate.
//
// SECURITY NOTE: the private key below is intentionally shipped with the
// software. It provides transport encryption and resistance to a casual
// man-in-the-middle, but NOT against an attacker who possesses the binary (they
// can extract the key and impersonate the server on the built-in-cert path).
// Agent identity remains protected by the per-agent token, which is the real
// secret; for stronger server authentication, front YALS with a real certificate
// (the CA-validation path).
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
