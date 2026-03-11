package parse

import (
	"fmt"
	"strings"

	"github.com/otfabric/opcua/ua"
)

// NodeID parses a string to a ua.NodeID using the standard OPC UA format.
func NodeID(s string) (*ua.NodeID, error) {
	if s == "" {
		return nil, fmt.Errorf("invalid node ID: empty string")
	}
	id, err := ua.ParseNodeID(s)
	if err != nil {
		return nil, fmt.Errorf("invalid node ID %q: %w", s, err)
	}
	// ParseNodeID may succeed for arbitrary strings; verify the round-trip.
	if rt := id.String(); rt != s && !strings.EqualFold(rt, s) {
		return nil, fmt.Errorf("invalid node ID %q", s)
	}
	return id, nil
}

// NodeIDs parses multiple node ID strings.
func NodeIDs(ss []string) ([]*ua.NodeID, error) {
	ids := make([]*ua.NodeID, len(ss))
	for i, s := range ss {
		id, err := NodeID(s)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

// SecurityPolicy normalizes a security policy name to a full URI.
func SecurityPolicy(s string) string {
	if s == "" || strings.EqualFold(s, "none") {
		return "http://opcfoundation.org/UA/SecurityPolicy#None"
	}
	if strings.HasPrefix(s, "http") {
		return s
	}
	return "http://opcfoundation.org/UA/SecurityPolicy#" + s
}

// SecurityPolicyShortName extracts the short name from a policy URI.
func SecurityPolicyShortName(uri string) string {
	if uri == "" {
		return "None"
	}
	if idx := strings.LastIndex(uri, "#"); idx >= 0 {
		return uri[idx+1:]
	}
	return uri
}

// NodeClassMask converts a node class filter string to ua.NodeClass.
func NodeClassMask(s string) ua.NodeClass {
	if s == "" || strings.EqualFold(s, "all") {
		return ua.NodeClassAll
	}
	return ua.NodeClassFromString(s)
}
