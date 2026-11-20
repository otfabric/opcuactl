// SPDX-License-Identifier: MIT

package opcuactl

import (
	"strings"
	"testing"

	"github.com/otfabric/go-opcua/id"
)

// TestAggregateNodeID maps every name of the completion, the aliases and
// NodeIds.
func TestAggregateNodeID(t *testing.T) {
	for _, name := range aggregateValues {
		if _, err := aggregateNodeID(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for in, want := range map[string]string{
		"min":         "i=2346",
		"MAX":         "i=2347",
		"TimeAverage": "i=2343",
		"i=2342":      "i=2342",
		"ns=2;s=Agg":  "ns=2;s=Agg",
	} {
		got, err := aggregateNodeID(in)
		if err != nil || got.String() != want {
			t.Errorf("%s: %v %v, want %s", in, got, err, want)
		}
	}
	if id.AggregateFunctionMinimum != 2346 || id.AggregateFunctionMaximum != 2347 {
		t.Fatal("the NodeIds of Minimum and Maximum changed")
	}
	for _, bad := range []string{"median", "ns=x;s=", ""} {
		if _, err := aggregateNodeID(bad); err == nil || !strings.Contains(err.Error(), "worstquality") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
