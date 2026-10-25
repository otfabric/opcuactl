// SPDX-License-Identifier: MIT

package model

// ServeInfo describes a server started by `opcuactl serve`.
type ServeInfo struct {
	ApplicationURI string `json:"applicationUri"`
	// DemoNamespace is the URI and index of the demo namespace, empty with
	// --no-demo.
	DemoNamespace      string `json:"demoNamespace,omitempty"`
	DemoNamespaceIndex uint16 `json:"demoNamespaceIndex,omitempty"`
	// Namespaces are the namespace URIs of the server, by index.
	Namespaces   []string           `json:"namespaces"`
	Certificates []ServeCertificate `json:"certificates,omitempty"`
	// ClientTrust is "any" when every client application certificate is
	// accepted, else the number of trusted certificates.
	ClientTrust string `json:"clientTrust"`
	// Users are the user names the server accepts; Anonymous whether it
	// accepts anonymous sessions.
	Anonymous bool       `json:"anonymous"`
	Users     []string   `json:"users,omitempty"`
	Endpoints []Endpoint `json:"endpoints"`
}

// ServeCertificate is an application instance certificate of the server.
type ServeCertificate struct {
	Thumbprint string `json:"thumbprint"`
	KeyType    string `json:"keyType"`
	Subject    string `json:"subject"`
	NotAfter   string `json:"notAfter"`
	// Generated is true for a self-signed certificate made at start.
	Generated bool `json:"generated"`
	// File is where the certificate was written (--cert-out) or read from.
	File string `json:"file,omitempty"`
}
