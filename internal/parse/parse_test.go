package parse

import "testing"

func TestNodeID(t *testing.T) {
	tests := []struct {
		input   string
		wantStr string
		wantErr bool
	}{
		{"i=2258", "i=2258", false},
		{"ns=2;i=1001", "ns=2;i=1001", false},
		{"ns=2;s=Temperature", "ns=2;s=Temperature", false},
		{"", "", true},
		{"invalid", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			id, err := NodeID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NodeID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err == nil && id.String() != tt.wantStr {
				t.Errorf("NodeID(%q) = %q, want %q", tt.input, id.String(), tt.wantStr)
			}
		})
	}
}

func TestNodeIDs(t *testing.T) {
	ids, err := NodeIDs([]string{"i=2258", "ns=2;i=1001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d IDs, want 2", len(ids))
	}

	_, err = NodeIDs([]string{"i=2258", "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid ID")
	}
}

func TestSecurityPolicy(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "http://opcfoundation.org/UA/SecurityPolicy#None"},
		{"none", "http://opcfoundation.org/UA/SecurityPolicy#None"},
		{"None", "http://opcfoundation.org/UA/SecurityPolicy#None"},
		{"Basic256Sha256", "http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256"},
		{"http://opcfoundation.org/UA/SecurityPolicy#Basic256", "http://opcfoundation.org/UA/SecurityPolicy#Basic256"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SecurityPolicy(tt.input)
			if got != tt.want {
				t.Errorf("SecurityPolicy(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSecurityPolicyShortName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "None"},
		{"http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256", "Basic256Sha256"},
		{"http://opcfoundation.org/UA/SecurityPolicy#None", "None"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SecurityPolicyShortName(tt.input)
			if got != tt.want {
				t.Errorf("SecurityPolicyShortName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
