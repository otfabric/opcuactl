// SPDX-License-Identifier: MIT

package parse

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/otfabric/go-opcua/ua"
)

// ReturnDiagnostics parses the ReturnDiagnostics mask of the requests
// (Part 4 §7.33): a comma-separated list of none, service, operation and
// all, or a number (decimal or 0x hex) for the raw mask bits.
func ReturnDiagnostics(s string) (uint32, error) {
	var mask uint32
	for _, part := range strings.Split(s, ",") {
		switch p := strings.ToLower(strings.TrimSpace(part)); p {
		case "", "none", "off":
		case "service":
			mask |= ua.ServiceLevelAll
		case "operation", "operations":
			mask |= ua.OperationLevelAll
		case "all":
			mask |= ua.ReturnDiagnosticsAll
		default:
			n, err := strconv.ParseUint(p, 0, 32)
			if err != nil || uint32(n)&^ua.ReturnDiagnosticsAll != 0 {
				return 0, fmt.Errorf("unknown diagnostics mask %q (none|service|operation|all or a number up to 0x3ff)", part)
			}
			mask |= uint32(n)
		}
	}
	return mask, nil
}
