// SPDX-License-Identifier: MIT

package opcuactl

import (
	"strings"
	"testing"
)

// OPCUACTL_INSECURE_NO_SECURITY sets the flag, which wins over it.
func TestInsecureNoSecurityEnv(t *testing.T) {
	t.Setenv("OPCUACTL_INSECURE_NO_SECURITY", "true")
	if _, _, err := runCLI(t, "version"); err != nil {
		t.Fatal(err)
	}
	if !connCfg.InsecureNoSecurity {
		t.Error("the variable did not set the option")
	}
	if _, _, err := runCLI(t, "version", "--insecure-no-security=false"); err != nil {
		t.Fatal(err)
	}
	if connCfg.InsecureNoSecurity {
		t.Error("the flag did not win over the variable")
	}

	t.Setenv("OPCUACTL_INSECURE_NO_SECURITY", "maybe")
	_, _, err := runCLI(t, "version")
	if err == nil || !strings.Contains(err.Error(), "OPCUACTL_INSECURE_NO_SECURITY") {
		t.Fatalf("error %v", err)
	}
}
