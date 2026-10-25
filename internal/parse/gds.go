// SPDX-License-Identifier: MIT

package parse

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

// TrustListMasks parses the lists of a trust list (Part 12 §7.8.2.7): values
// of trusted, trusted-crls, issuers, issuer-crls and all, each possibly a
// comma-separated list. No values select all lists.
func TrustListMasks(values []string) (ua.TrustListMasks, error) {
	var m ua.TrustListMasks
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			switch strings.ToLower(strings.TrimSpace(part)) {
			case "":
			case "trusted", "trusted-certs", "trusted-certificates":
				m |= ua.TrustListMasksTrustedCertificates
			case "trusted-crls", "trusted-crl", "crls", "crl":
				m |= ua.TrustListMasksTrustedCrls
			case "issuers", "issuer", "issuer-certs", "issuer-certificates":
				m |= ua.TrustListMasksIssuerCertificates
			case "issuer-crls", "issuer-crl":
				m |= ua.TrustListMasksIssuerCrls
			case "all":
				m |= ua.TrustListMasksAll
			default:
				return 0, fmt.Errorf("unknown trust list %q (trusted|trusted-crls|issuers|issuer-crls|all)", part)
			}
		}
	}
	if m == 0 {
		m = ua.TrustListMasksAll
	}
	return m, nil
}

// CertificateTypes are the names of the application certificate types (Part
// 12 §7.8.4) for --type, with their NodeIds in namespace 0.
var CertificateTypes = []struct {
	Name string
	ID   uint32
}{
	{"rsa", id.RsaSha256ApplicationCertificateType},
	{"rsa-min", id.RsaMinApplicationCertificateType},
	{"ecc-p256", id.EccNistP256ApplicationCertificateType},
	{"ecc-p384", id.EccNistP384ApplicationCertificateType},
	{"ecc-brainpool-p256", id.EccBrainpoolP256r1ApplicationCertificateType},
	{"ecc-brainpool-p384", id.EccBrainpoolP384r1ApplicationCertificateType},
	{"ed25519", id.EccCurve25519ApplicationCertificateType},
	{"ed448", id.EccCurve448ApplicationCertificateType},
}

// CertificateType parses --type: one of the names of CertificateTypes
// (rsa-sha256, curve25519 and curve448 are accepted too), or a NodeId. An
// empty string is nil, the null NodeId of the methods.
func CertificateType(s string) (*ua.NodeID, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "":
		return nil, nil
	case "rsa-sha256":
		s = "rsa"
	case "curve25519", "ecc-curve25519":
		s = "ed25519"
	case "curve448", "ecc-curve448":
		s = "ed448"
	}
	for _, t := range CertificateTypes {
		if strings.EqualFold(s, t.Name) {
			return ua.NewNumericNodeID(0, t.ID), nil
		}
	}
	n, err := NodeID(s)
	if err != nil {
		names := make([]string, len(CertificateTypes))
		for i, t := range CertificateTypes {
			names[i] = t.Name
		}
		return nil, fmt.Errorf("unknown certificate type %q (%s or a NodeId)", s, strings.Join(names, "|"))
	}
	return n, nil
}

// CertificateTypeName returns the name of a certificate type NodeId: the
// --type name of the types of CertificateTypes, the BrowseName of other
// types of namespace 0, the NodeId otherwise.
func CertificateTypeName(n *ua.NodeID) string {
	if n == nil {
		return ""
	}
	if n.Namespace() == 0 && n.Type() <= ua.NodeIDTypeNumeric {
		for _, t := range CertificateTypes {
			if t.ID == n.IntID() {
				return t.Name
			}
		}
		if name := id.Name(n.IntID()); name != strconv.FormatUint(uint64(n.IntID()), 10) {
			return name
		}
	}
	return n.String()
}
