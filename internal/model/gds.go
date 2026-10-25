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
	List            string `json:"list"`
	Group           string `json:"group,omitempty"`           // gds certs: the certificate group
	CertificateType string `json:"certificateType,omitempty"` // gds certs: the certificate type
	Kind            string `json:"kind"`                      // certificate or crl
	Subject         string `json:"subject,omitempty"`
	Issuer          string `json:"issuer,omitempty"`
	ApplicationURI  string `json:"applicationUri,omitempty"`
	Thumbprint      string `json:"thumbprint"`
	NotBefore       string `json:"notBefore,omitempty"`
	NotAfter        string `json:"notAfter,omitempty"`
	CA              bool   `json:"ca,omitempty"`
	Revoked         int    `json:"revoked,omitempty"` // CRL: revoked certificates
	File            string `json:"file,omitempty"`    // where --out wrote it
	Error           string `json:"error,omitempty"`   // the entry does not parse
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

// GDSTransaction are the TransactionDiagnostics of ServerConfiguration
// (Part 12 §7.10.11): the last certificate management transaction.
type GDSTransaction struct {
	StartTime                 string                `json:"startTime,omitempty"`
	EndTime                   string                `json:"endTime,omitempty"`
	Result                    string                `json:"result"`
	AffectedTrustLists        []string              `json:"affectedTrustLists"`
	AffectedCertificateGroups []string              `json:"affectedCertificateGroups"`
	Errors                    []GDSTransactionError `json:"errors"`
}

// GDSTransactionError is an error of a transaction: the trust list,
// certificate group or certificate it concerns.
type GDSTransactionError struct {
	Target  string `json:"target"`
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// GDSCertificateGroup is a certificate group of ServerConfiguration.
type GDSCertificateGroup struct {
	Name             string   `json:"name"`
	NodeID           string   `json:"nodeId"`
	TrustList        string   `json:"trustList"`
	CertificateTypes []string `json:"certificateTypes"`
}

// GDSApplication is an application record of a Global Discovery Server
// (ApplicationRecordDataType, Part 12 §6.6.3).
type GDSApplication struct {
	ApplicationID      string   `json:"applicationId,omitempty"`
	ApplicationURI     string   `json:"applicationUri"`
	ApplicationType    string   `json:"applicationType"`
	ApplicationNames   []string `json:"applicationNames"`
	ProductURI         string   `json:"productUri,omitempty"`
	DiscoveryURLs      []string `json:"discoveryUrls"`
	ServerCapabilities []string `json:"serverCapabilities"`
}

// GDSApplicationQuery is the result of QueryApplications.
type GDSApplicationQuery struct {
	LastCounterResetTime string           `json:"lastCounterResetTime,omitempty"`
	NextRecordID         uint32           `json:"nextRecordId"`
	Applications         []GDSApplication `json:"applications"`
}

// GDSServer is a server of QueryServers.
type GDSServer struct {
	RecordID           uint32   `json:"recordId"`
	ServerName         string   `json:"serverName"`
	DiscoveryURL       string   `json:"discoveryUrl"`
	ServerCapabilities []string `json:"serverCapabilities"`
}

// GDSServerQuery is the result of QueryServers.
type GDSServerQuery struct {
	LastCounterResetTime string      `json:"lastCounterResetTime,omitempty"`
	Servers              []GDSServer `json:"servers"`
}

// GDSRequest is a certificate request of a GDS.
type GDSRequest struct {
	ApplicationID string `json:"applicationId"`
	RequestID     string `json:"requestId"`
	Kind          string `json:"kind"` // signing-request or new-key-pair
}

// GDSIssued is a certificate a GDS issued (FinishRequest, Enroll) and where
// opcuactl wrote it.
type GDSIssued struct {
	ApplicationID string           `json:"applicationId"`
	RequestID     string           `json:"requestId,omitempty"`
	Certificate   GDSCertificate   `json:"certificate"`
	Issuers       []GDSCertificate `json:"issuers"`
	CertFile      string           `json:"certFile,omitempty"`
	KeyFile       string           `json:"keyFile,omitempty"`
	IssuersFile   string           `json:"issuersFile,omitempty"`
	PrivateKeyPEM string           `json:"privateKeyPem,omitempty"` // a new key pair that --key-out did not take
	TrustList     []GDSCertificate `json:"trustList,omitempty"`
	TrustDir      string           `json:"trustDir,omitempty"`
}

// GDSCertificateStatus is the result of GetCertificateStatus.
type GDSCertificateStatus struct {
	ApplicationID   string `json:"applicationId"`
	Group           string `json:"group,omitempty"`
	CertificateType string `json:"certificateType,omitempty"`
	UpdateRequired  bool   `json:"updateRequired"`
}

// GDSRevocationStatus is the result of CheckRevocationStatus.
type GDSRevocationStatus struct {
	Thumbprint string `json:"thumbprint"`
	Subject    string `json:"subject,omitempty"`
	Status     string `json:"status"`
	ValidUntil string `json:"validUntil,omitempty"`
}
