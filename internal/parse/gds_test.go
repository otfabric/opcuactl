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

func TestCertificateGroup(t *testing.T) {
	for _, tt := range []struct {
		in, group string
		trust     bool
	}{
		{"", "i=14156", true},
		{"App", "i=14156", true},
		{"https", "i=14088", true},
		{"user-token", "i=14122", true},
		{"i=14088", "i=14088", true},
		{"DefaultHttpsGroup", "i=14088", true},
		{"ns=2;s=Groups.Mqtt", "ns=2;s=Groups.Mqtt", false},
	} {
		g, tl, err := CertificateGroup(tt.in)
		if err != nil || g.String() != tt.group || (tl != nil) != tt.trust {
			t.Errorf("CertificateGroup(%q) = %v, %v, %v", tt.in, g, tl, err)
		}
	}
	if _, _, err := CertificateGroup("nope nope"); err == nil {
		t.Error("an invalid group parsed")
	}
	for in, want := range map[string]string{"i=14156": "default", "i=14088": "https", "i=14122": "usertoken", "ns=2;s=G": "ns=2;s=G"} {
		n, _ := NodeID(in)
		if got := CertificateGroupName(n); got != want {
			t.Errorf("CertificateGroupName(%s) = %q, want %q", in, got, want)
		}
	}
	if CertificateGroupName(nil) != "" {
		t.Error("CertificateGroupName(nil)")
	}
}

func TestApplicationType(t *testing.T) {
	for in, want := range map[string]ua.ApplicationType{
		"": ua.ApplicationTypeServer, "Client": ua.ApplicationTypeClient, "both": ua.ApplicationTypeClientAndServer,
		"discovery": ua.ApplicationTypeDiscoveryServer,
	} {
		got, err := ApplicationType(in)
		if err != nil || got != want {
			t.Errorf("ApplicationType(%q) = %v, %v", in, got, err)
		}
		if back, _ := ApplicationType(ApplicationTypeName(got)); back != got {
			t.Errorf("ApplicationTypeName(%v) = %q", got, ApplicationTypeName(got))
		}
	}
	if _, err := ApplicationType("plc"); err == nil {
		t.Error("plc parsed")
	}
	if ApplicationTypeName(7) != "7" {
		t.Error("ApplicationTypeName(7)")
	}
}
