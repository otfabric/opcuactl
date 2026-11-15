// SPDX-License-Identifier: MIT

package conn

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	uaerrors "github.com/otfabric/go-opcua/errors"
	"github.com/otfabric/go-opcua/gds"
)

func testCertDER(t *testing.T) []byte {
	t.Helper()
	certPath, _ := writeTestCertKey(t, t.TempDir())
	certs, err := loadCertificatesFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	return certs[0].Raw
}

// A ServerCertificate may carry the chain after the server's own
// certificate: the thumbprint is the one of the first certificate.
func TestLeafCertificate(t *testing.T) {
	leaf, issuer := testCertDER(t), testCertDER(t)
	chain := append(append([]byte{}, leaf...), issuer...)
	if got := LeafCertificate(chain); !bytes.Equal(got, leaf) {
		t.Errorf("chain: %d bytes, want the %d of the leaf", len(got), len(leaf))
	}
	if got := LeafCertificate(leaf); !bytes.Equal(got, leaf) {
		t.Error("a single certificate is not returned as it is")
	}
	for _, b := range [][]byte{nil, {}, {0x30}, {0xff, 0x01}} {
		if got := LeafCertificate(b); !bytes.Equal(got, b) {
			t.Errorf("% x: %x", b, got)
		}
	}
	if got, want := Thumbprint(LeafCertificate(chain)), gds.Thumbprint(leaf); got != want || len(got) != 40 || got != strings.ToUpper(got) {
		t.Errorf("thumbprint %q, want %q", got, want)
	}
	if Thumbprint(nil) != "" {
		t.Error("a thumbprint of nothing")
	}
}

func TestUntrusted(t *testing.T) {
	der := testCertDER(t)
	noList := fmt.Errorf("validation failed: %w: no trusted certificates configured, see TrustedCertificates", uaerrors.ErrInvalidCertificate)
	unknown := fmt.Errorf("validation failed: %w", x509.UnknownAuthorityError{})

	cfg := &Config{remoteCert: der}
	err := cfg.untrusted(noList)
	var u *UntrustedCertificateError
	if !errors.As(err, &u) || !u.NoTrustList || u.Thumbprint != gds.Thumbprint(der) || !errors.Is(err, uaerrors.ErrInvalidCertificate) {
		t.Fatalf("no trust list: %#v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "no --trusted-cert is given") || !strings.Contains(msg, u.Thumbprint) || strings.Contains(msg, "TrustedCertificates") {
		t.Errorf("message %q", msg)
	}

	cfg.TrustedCertFiles = []string{"ca.pem"}
	if cfg.untrusted(noList) != nil {
		t.Error("an invalid certificate with a trust list is not a trust list problem")
	}
	err = cfg.untrusted(unknown)
	if !errors.As(err, &u) || u.NoTrustList || !strings.Contains(err.Error(), "not one of the --trusted-cert certificates") {
		t.Errorf("unknown authority: %v", err)
	}

	cfg.remoteCert = nil
	if err := cfg.untrusted(unknown); err == nil || strings.Contains(err.Error(), "SHA-1 thumbprint") {
		t.Errorf("without a known certificate: %v", err)
	}
	if cfg.untrusted(os.ErrNotExist) != nil {
		t.Error("another error is taken for a trust problem")
	}
}
