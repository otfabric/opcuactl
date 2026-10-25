// SPDX-License-Identifier: MIT

package model

// GDSProperties are the properties of the ServerConfiguration object (OPC UA
// Part 12 §7.10.3) and the certificate types of the DefaultApplicationGroup.
type GDSProperties struct {
	ServerCapabilities         []string `json:"serverCapabilities"`
	SupportedPrivateKeyFormats []string `json:"supportedPrivateKeyFormats"`
	MaxTrustListSize           uint32   `json:"maxTrustListSize"`
	MulticastDNSEnabled        bool     `json:"multicastDnsEnabled"`
	CertificateTypes           []string `json:"certificateTypes"`
}

// GDSCertificate is a certificate or CRL of a trust list or the rejected
// list. List is trusted, trusted-crl, issuer, issuer-crl or rejected.
type GDSCertificate struct {
	List           string `json:"list"`
	Kind           string `json:"kind"` // certificate or crl
	Subject        string `json:"subject,omitempty"`
	Issuer         string `json:"issuer,omitempty"`
	ApplicationURI string `json:"applicationUri,omitempty"`
	Thumbprint     string `json:"thumbprint"`
	NotBefore      string `json:"notBefore,omitempty"`
	NotAfter       string `json:"notAfter,omitempty"`
	CA             bool   `json:"ca,omitempty"`
	Revoked        int    `json:"revoked,omitempty"` // CRL: revoked certificates
	File           string `json:"file,omitempty"`    // where --out wrote it
	Error          string `json:"error,omitempty"`   // the entry does not parse
}

// GDSResult is the outcome of a change of the server's certificates.
type GDSResult struct {
	Action               string   `json:"action"`
	Thumbprints          []string `json:"thumbprints,omitempty"`
	ApplyChangesRequired bool     `json:"applyChangesRequired"`
	Applied              bool     `json:"applied"`
}

// GDSSigningRequest is the certificate signing request CreateSigningRequest
// returned.
type GDSSigningRequest struct {
	CertificateType string   `json:"certificateType,omitempty"`
	Subject         string   `json:"subject"`
	URIs            []string `json:"uris,omitempty"`
	DNSNames        []string `json:"dnsNames,omitempty"`
	IPAddresses     []string `json:"ipAddresses,omitempty"`
	KeyAlgorithm    string   `json:"keyAlgorithm"`
	PEM             string   `json:"pem"`
	File            string   `json:"file,omitempty"`
}
