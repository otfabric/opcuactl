// SPDX-License-Identifier: MIT

package serve

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"time"

	"github.com/otfabric/go-opcua/gds"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uapolicy"
)

// Identity is an application instance certificate of the server with its
// private key.
type Identity struct {
	// Cert is the DER encoded certificate.
	Cert []byte
	// Key is the private key of Cert.
	Key crypto.Signer
	// Generated is true for a self-signed certificate made by
	// GenerateIdentities.
	Generated bool
}

// Thumbprint returns the SHA-1 thumbprint of the certificate (Part 6
// §6.2.3) in upper-case hex, as GDS tools print it.
func (id Identity) Thumbprint() string { return gds.Thumbprint(id.Cert) }

// KeyType names the key type of the certificate: rsa, ecc-p256, ecc-p384,
// ed25519 or another key type of uapolicy.
func (id Identity) KeyType() string { return keyTypeName(id.Key) }

func keyTypeName(key crypto.Signer) string {
	switch k := key.Public().(type) {
	case *rsa.PublicKey:
		return "rsa"
	case *ecdsa.PublicKey:
		return "ecc-" + k.Curve.Params().Name
	case ed25519.PublicKey:
		return "ed25519"
	default:
		return fmt.Sprintf("%T", k)
	}
}

// generatable are the key types GenerateIdentities can make, cheapest
// first: they are tried in this order for a security policy.
var generatable = []string{"ecc-P-256", "ecc-P-384", "ed25519", "rsa"}

func generateKey(kind string) (crypto.Signer, error) {
	switch kind {
	case "ecc-P-256":
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "ecc-P-384":
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case "ed25519":
		_, key, err := ed25519.GenerateKey(rand.Reader)
		return key, err
	default:
		return rsa.GenerateKey(rand.Reader, 2048)
	}
}

// GenerateIdentities returns the self-signed identities the server needs in
// addition to have: one for every security policy of policies that no
// certificate in have serves, and an RSA one when needRSA is set (the TLS
// certificate of opc.wss and opc.https) and have has none. The certificates
// carry appURI and the host names and IP addresses of hosts. A policy no
// generatable key type serves (the Brainpool and Curve448 ones) is an
// error: its certificate has to be given.
func GenerateIdentities(have []Identity, policies []string, needRSA bool, appURI string, hosts []string) ([]Identity, error) {
	covered := func(pub crypto.PublicKey, policy string) bool {
		return uapolicy.CheckPublicKey(policy, pub) == nil
	}
	served := func(policy string, ids []Identity) bool {
		for _, id := range ids {
			if covered(id.Key.Public(), policy) {
				return true
			}
		}
		return false
	}
	keys := map[string]crypto.Signer{}
	var made []string
	use := func(kind string) {
		for _, k := range made {
			if k == kind {
				return
			}
		}
		made = append(made, kind)
	}
	for _, policy := range policies {
		if policy == ua.SecurityPolicyURINone || served(policy, have) {
			continue
		}
		found := false
		for _, kind := range generatable {
			key := keys[kind]
			if key == nil {
				var err error
				if key, err = generateKey(kind); err != nil {
					return nil, fmt.Errorf("generating a %s key: %w", kind, err)
				}
				keys[kind] = key
			}
			if covered(key.Public(), policy) {
				use(kind)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("security policy %s needs a certificate of its key type: give it with --cert and --key", shortPolicy(policy))
		}
	}
	if needRSA {
		hasRSA := false
		for _, id := range have {
			if _, ok := id.Key.(*rsa.PrivateKey); ok {
				hasRSA = true
			}
		}
		if !hasRSA {
			if keys["rsa"] == nil {
				key, err := generateKey("rsa")
				if err != nil {
					return nil, fmt.Errorf("generating an rsa key: %w", err)
				}
				keys["rsa"] = key
			}
			use("rsa")
		}
	}
	// RSA first: it is the certificate go-opcua uses on unsecured channels
	// and for TLS.
	var out []Identity
	for _, kind := range []string{"rsa", "ecc-P-256", "ecc-P-384", "ed25519"} {
		for _, k := range made {
			if k != kind {
				continue
			}
			der, err := selfSigned(keys[kind], appURI, hosts)
			if err != nil {
				return nil, err
			}
			out = append(out, Identity{Cert: der, Key: keys[kind], Generated: true})
		}
	}
	return out, nil
}

// selfSigned creates a self-signed application instance certificate (Part 6
// §6.2.2) for appURI and hosts, valid for a year.
func selfSigned(key crypto.Signer, appURI string, hosts []string) ([]byte, error) {
	u, err := url.Parse(appURI)
	if err != nil {
		return nil, fmt.Errorf("application URI %q: %w", appURI, err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "opcuactl serve", Organization: []string{"opcuactl"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment |
			x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{u},
	}
	if _, ok := key.(ed25519.PrivateKey); ok {
		// Ed25519 keys only sign (RFC 8410 §5).
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment | x509.KeyUsageCertSign
	}
	if _, ok := key.(*ecdsa.PrivateKey); ok {
		tmpl.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment | x509.KeyUsageCertSign
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("creating a self-signed certificate: %w", err)
	}
	return der, nil
}

// ParseIdentities pairs the certificates of certPEM (PEM, several blocks
// allowed, or one DER certificate) with the private keys of keyPEM (PEM,
// several blocks allowed, or one DER key): every key needs its certificate;
// certificates without a key (issuers of a chain) are left out.
func ParseIdentities(certPEM, keyPEM []byte) ([]Identity, error) {
	certs, err := parseCertificates(certPEM)
	if err != nil {
		return nil, err
	}
	keys, err := parseKeys(keyPEM)
	if err != nil {
		return nil, err
	}
	var out []Identity
	for _, key := range keys {
		matched := false
		for _, c := range certs {
			if uapolicy.KeyMatchesCertificate(key, c) {
				out = append(out, Identity{Cert: c, Key: key})
				matched = true
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("no certificate for the %s private key", keyTypeName(key))
		}
	}
	return out, nil
}

// ParseCertificates returns the DER certificates of data: PEM (several
// blocks allowed) or one DER certificate.
func ParseCertificates(data []byte) ([][]byte, error) { return parseCertificates(data) }

func parseCertificates(data []byte) ([][]byte, error) {
	var out [][]byte
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if _, err := uapolicy.ParseCertificate(block.Bytes); err != nil {
			return nil, err
		}
		out = append(out, block.Bytes)
	}
	if len(out) > 0 {
		return out, nil
	}
	if _, err := uapolicy.ParseCertificate(data); err != nil {
		return nil, fmt.Errorf("no certificate: %w", err)
	}
	return [][]byte{data}, nil
}

func parseKeys(data []byte) ([]crypto.Signer, error) {
	var out []crypto.Signer
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		key, err := uapolicy.ParsePrivateKey(pem.EncodeToMemory(block))
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	if len(out) > 0 {
		return out, nil
	}
	key, err := uapolicy.ParsePrivateKey(data)
	if err != nil {
		return nil, err
	}
	return []crypto.Signer{key}, nil
}

// EncodeCertificates returns the certificates of ids as PEM.
func EncodeCertificates(ids []Identity) []byte {
	var out []byte
	for _, id := range ids {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: id.Cert})...)
	}
	return out
}

// EncodeKeys returns the private keys of ids as PKCS #8 PEM, in the order
// of their certificates.
func EncodeKeys(ids []Identity) ([]byte, error) {
	var out []byte
	for _, id := range ids {
		der, err := uapolicy.MarshalPrivateKey(id.Key)
		if err != nil {
			return nil, err
		}
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})...)
	}
	return out, nil
}
