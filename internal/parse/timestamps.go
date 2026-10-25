// SPDX-License-Identifier: MIT

package parse

import (
	"fmt"
	"strings"

	"github.com/otfabric/go-opcua/ua"
)

// TimestampsToReturn parses the TimestampsToReturn of a Read or a
// MonitoredItem (Part 4 §7.40): source, server, both or neither, in any
// case. The empty string is both.
func TimestampsToReturn(s string) (ua.TimestampsToReturn, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "both":
		return ua.TimestampsToReturnBoth, nil
	case "source":
		return ua.TimestampsToReturnSource, nil
	case "server":
		return ua.TimestampsToReturnServer, nil
	case "neither":
		return ua.TimestampsToReturnNeither, nil
	}
	return ua.TimestampsToReturnInvalid, fmt.Errorf("unknown timestamps to return %q (source|server|both|neither)", s)
}
