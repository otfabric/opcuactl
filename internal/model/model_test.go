// SPDX-License-Identifier: MIT

package model

import (
	"strings"
	"testing"
	"time"
)

func TestFormatTimestamp(t *testing.T) {
	t.Parallel()
	if got := FormatTimestamp(time.Time{}); got != "" {
		t.Errorf("FormatTimestamp(zero) = %q, want empty", got)
	}
	ts := time.Date(2026, 3, 25, 12, 30, 45, 123456789, time.UTC)
	got := FormatTimestamp(ts)
	if !strings.HasPrefix(got, "2026-03-25T12:30:45") {
		t.Errorf("FormatTimestamp = %q, want RFC3339Nano prefix 2026-03-25T12:30:45", got)
	}
}
