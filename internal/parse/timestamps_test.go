// SPDX-License-Identifier: MIT

package parse

import (
	"testing"

	"github.com/otfabric/go-opcua/ua"
)

func TestTimestampsToReturn(t *testing.T) {
	for in, want := range map[string]ua.TimestampsToReturn{
		"": ua.TimestampsToReturnBoth, "both": ua.TimestampsToReturnBoth, "Both": ua.TimestampsToReturnBoth,
		"source": ua.TimestampsToReturnSource, "SERVER": ua.TimestampsToReturnServer, " neither ": ua.TimestampsToReturnNeither,
	} {
		got, err := TimestampsToReturn(in)
		if err != nil || got != want {
			t.Errorf("TimestampsToReturn(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := TimestampsToReturn("sometimes"); err == nil {
		t.Error("want an error for an unknown value")
	}
}
