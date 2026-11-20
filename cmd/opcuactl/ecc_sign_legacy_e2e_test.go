// SPDX-License-Identifier: MIT

package opcuactl

import (
	"path/filepath"
	"testing"
)

// TestE2E_ECCSignLegacyKeyLength connects in the mode Sign of ECC_nistP256:
// a server and a client that both derive the keys as Part 6 §6.8.1 asks, or
// both as open62541 v1.5.9 did (--ecc-sign-legacy-key-length), agree; a
// client that derives them the other way is refused.
func TestE2E_ECCSignLegacyKeyLength(t *testing.T) {
	cli := newIdentity(t, "p256", clientURI)
	for _, legacyServer := range []bool{false, true} {
		dir := t.TempDir()
		certOut := filepath.Join(dir, "server.pem")
		args := []string{"--no-demo", "--security-policy", "ECC_nistP256", "--security-mode", "Sign", "--cert-out", certOut}
		if legacyServer {
			args = append(args, "--ecc-sign-legacy-key-length")
		}
		s := startServe(t, args...)
		ep := s.url(t, "http://opcfoundation.org/UA-Profile/Transport/uatcp-uasc-uabinary")
		get := []string{"get", "value", "-e", ep, "-n", "i=2258", "-q", "--security-policy", "ECC_nistP256", "--security-mode", "Sign",
			"--cert", cli.certFile, "--key", cli.keyFile, "--trusted-cert", certOut}
		legacy := append(append([]string{}, get...), "--ecc-sign-legacy-key-length")
		match, mismatch := get, legacy
		if legacyServer {
			match, mismatch = legacy, get
		}
		mustRun(t, match...)
		if _, _, err := runCLI(t, mismatch...); err == nil {
			t.Errorf("legacy server %v: a client that derives the keys the other way connected", legacyServer)
		}
	}
}
