// SPDX-License-Identifier: MIT

package parse

import (
	"testing"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

func TestTrustListMasks(t *testing.T) {
	for _, tt := range []struct {
		in      []string
		want    ua.TrustListMasks
		wantErr bool
	}{
		{nil, ua.TrustListMasksAll, false},
		{[]string{""}, ua.TrustListMasksAll, false},
		{[]string{"trusted"}, ua.TrustListMasksTrustedCertificates, false},
		{[]string{"Trusted-CRLs"}, ua.TrustListMasksTrustedCrls, false},
		{[]string{"issuers,issuer-crls"}, ua.TrustListMasksIssuerCertificates | ua.TrustListMasksIssuerCrls, false},
		{[]string{"trusted", " issuer "}, ua.TrustListMasksTrustedCertificates | ua.TrustListMasksIssuerCertificates, false},
		{[]string{"all"}, ua.TrustListMasksAll, false},
		{[]string{"rejected"}, 0, true},
	} {
		got, err := TrustListMasks(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("TrustListMasks(%q) = %v, %v; want %v (error %v)", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestCertificateType(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    uint32
		wantNil bool
		wantErr bool
	}{
		{"", 0, true, false},
		{"rsa", id.RsaSha256ApplicationCertificateType, false, false},
		{"RSA-SHA256", id.RsaSha256ApplicationCertificateType, false, false},
		{"rsa-min", id.RsaMinApplicationCertificateType, false, false},
		{"ecc-p256", id.EccNistP256ApplicationCertificateType, false, false},
		{"ecc-p384", id.EccNistP384ApplicationCertificateType, false, false},
		{"ecc-brainpool-p256", id.EccBrainpoolP256r1ApplicationCertificateType, false, false},
		{"ecc-brainpool-p384", id.EccBrainpoolP384r1ApplicationCertificateType, false, false},
		{"curve25519", id.EccCurve25519ApplicationCertificateType, false, false},
		{"ed448", id.EccCurve448ApplicationCertificateType, false, false},
		{"ecc-curve448", id.EccCurve448ApplicationCertificateType, false, false},
		{"i=12557", 12557, false, false},
		{"dsa", 0, false, true},
	} {
		got, err := CertificateType(tt.in)
		switch {
		case (err != nil) != tt.wantErr:
			t.Errorf("CertificateType(%q) error = %v", tt.in, err)
		case tt.wantErr:
		case tt.wantNil && got != nil:
			t.Errorf("CertificateType(%q) = %v, want nil", tt.in, got)
		case !tt.wantNil && (got == nil || got.IntID() != tt.want):
			t.Errorf("CertificateType(%q) = %v, want %d", tt.in, got, tt.want)
		}
	}
}

func TestCertificateTypeName(t *testing.T) {
	for _, tt := range []struct {
		in   *ua.NodeID
		want string
	}{
		{nil, ""},
		{ua.NewNumericNodeID(0, id.RsaSha256ApplicationCertificateType), "rsa"},
		{ua.NewNumericNodeID(0, id.EccCurve25519ApplicationCertificateType), "ed25519"},
		{ua.NewNumericNodeID(0, id.ApplicationCertificateType), "ApplicationCertificateType"},
		{ua.NewNumericNodeID(0, 999999), "i=999999"},
		{ua.NewStringNodeID(2, "MyType"), "ns=2;s=MyType"},
	} {
		if got := CertificateTypeName(tt.in); got != tt.want {
			t.Errorf("CertificateTypeName(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
