// SPDX-License-Identifier: MIT

package parse

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// EventID parses the EventId of a condition event: base64 (as opcuactl
// prints it) or hex with a 0x prefix. "latest" and "" return latest = true:
// the EventId of the latest event of the condition is to be looked up.
func EventID(s string) (id []byte, latest bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "latest") {
		return nil, true, nil
	}
	if h, ok := strings.CutPrefix(strings.ToLower(s), "0x"); ok {
		b, err := hex.DecodeString(h)
		if err != nil || len(b) == 0 {
			return nil, false, fmt.Errorf("invalid hex EventId %q", s)
		}
		return b, false, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) > 0 {
			return b, false, nil
		}
	}
	return nil, false, fmt.Errorf("invalid EventId %q (base64, 0x hex or latest)", s)
}
