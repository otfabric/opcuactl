// SPDX-License-Identifier: MIT

package conn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/brainpool"
	"github.com/cloudflare/circl/sign/ed448"
	"github.com/otfabric/go-opcua/uapolicy"
)

// writeCertKey writes a self-signed certificate for key and its PKCS #8 key.
func writeCertKey(t *testing.T, key crypto.Signer) (certFile, keyFile string) {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "opcuactl test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := uapolicy.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := uapolicy.MarshalPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func TestLoadCertKey_extendedCurves(t *testing.T) {
	bp256, err := ecdsa.GenerateKey(brainpool.P256r1(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bp384, err := ecdsa.GenerateKey(brainpool.P384r1(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, ed, err := ed448.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]crypto.Signer{"brainpoolP256r1": bp256, "brainpoolP384r1": bp384, "ed448": ed} {
		t.Run(name, func(t *testing.T) {
			certFile, keyFile := writeCertKey(t, key)
			cert, got, err := loadCertKey(certFile, keyFile)
			if err != nil {
				t.Fatal(err)
			}
			if !uapolicy.KeyMatchesCertificate(got, cert) {
				t.Fatal("loaded key does not match the certificate")
			}
			trusted, err := loadTrustedCertificates([]string{certFile})
			if err != nil || len(trusted) != 1 {
				t.Fatalf("trusted: %v, %d certs", err, len(trusted))
			}
		})
	}
}

func TestLoadCertKey_mismatch(t *testing.T) {
	a, err := ecdsa.GenerateKey(brainpool.P256r1(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ecdsa.GenerateKey(brainpool.P256r1(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certFile, _ := writeCertKey(t, a)
	_, keyFile := writeCertKey(t, b)
	if _, _, err := loadCertKey(certFile, keyFile); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("want a mismatch error, got %v", err)
	}
	if _, _, err := loadCertKey(certFile, filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("want an error for a missing key file")
	}
	if _, _, err := loadCertKey(certFile, certFile); err == nil {
		t.Fatal("want an error for a certificate as key")
	}
}
