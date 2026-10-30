// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"syscall"
	"testing"

	uaerrors "github.com/otfabric/go-opcua/errors"
	"github.com/otfabric/go-opcua/ua"
)

func TestExitCodes(t *testing.T) {
	pathErr := &fs.PathError{Op: "open", Path: "x", Err: fs.ErrNotExist}
	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain error", errors.New("unknown flag"), exitUsage},
		{"usage", exitCode(exitUsage, errors.New("x")), exitUsage},
		{"missing file", exitCode(exitUsage, pathErr), exitInvalidInput},
		{"transport", exitCode(exitTransport, errors.New("x")), exitTransport},
		{"a file in a transport error", exitCode(exitTransport, pathErr), exitTransport},
		{"deadline", exitCode(exitTransport, context.DeadlineExceeded), exitTimeout},
		{"Bad_Timeout", exitCode(exitProtocol, ua.StatusBadTimeout), exitTimeout},
		{"unsupported", exitCode(exitProtocol, ua.StatusBadServiceUnsupported), exitUnsupported},
		{"access denied", exitCode(exitProtocol, ua.StatusBadUserAccessDenied), exitPermission},
		{"permission", exitCode(exitTransport, fs.ErrPermission), exitPermission},
		{"interrupted", exitCode(exitTransport, fmt.Errorf("x: %w", context.Canceled)), exitInterrupted},
		{"plain interrupted", fmt.Errorf("x: %w", context.Canceled), exitInterrupted},
		{"partial stays", exitCode(exitPartial, ua.StatusBadUserAccessDenied), exitPartial},
		{"findings stay", exitCode(exitFindings, ua.StatusBadTimeout), exitFindings},
		{"wrapped", fmt.Errorf("x: %w", exitCode(exitProtocol, errors.New("y"))), exitProtocol},
		{"connection lost", exitCodef(exitProtocol, "x: %w", io.EOF), exitTransport},
		{"connection reset", exitCode(exitProtocol, syscall.ECONNRESET), exitTransport},
		{"channel closed", exitCode(exitProtocol, uaerrors.ErrSecureChannelClosed), exitTransport},
		{"Bad_ConnectionClosed", exitCode(exitProtocol, ua.StatusBadConnectionClosed), exitTransport},
		{"not connected", exitCode(exitProtocol, ua.StatusBadServerNotConnected), exitTransport},
		{"a Bad status stays", exitCode(exitProtocol, ua.StatusBadNodeIDUnknown), exitProtocol},
		{"EOF of a file", exitCode(exitInvalidInput, io.EOF), exitInvalidInput},
	} {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, got, c.want)
		}
	}
	if exitCode(exitUsage, nil) != nil {
		t.Error("a nil error must stay nil")
	}
}

func TestRootContext(t *testing.T) {
	type key struct{}
	want := context.WithValue(context.Background(), key{}, 1)
	old := rootCtx.Load()
	t.Cleanup(func() { rootCtx.Store(old) })
	rootCtx.Store(&want)
	if rootContext().Value(key{}) != 1 {
		t.Fatal("rootContext is not the Execute context")
	}
	rootCtx.Store(nil)
	if rootContext() != context.Background() {
		t.Fatal("no Execute: want the background context")
	}
}

func TestFormatEnv(t *testing.T) {
	cliMu.Lock()
	defer cliMu.Unlock()
	resetFlags(rootCmd)
	t.Cleanup(func() { resetFlags(rootCmd) })
	t.Setenv("OPCUACTL_FORMAT", "")
	t.Setenv("OPCUACTL_OUTPUT_FORMAT", "json")
	if err := applyEnvOverrides(rootCmd); err != nil || outputFormat != "json" {
		t.Fatalf("OPCUACTL_OUTPUT_FORMAT ignored: %q, %v", outputFormat, err)
	}
	t.Setenv("OPCUACTL_FORMAT", "yaml")
	if err := applyEnvOverrides(rootCmd); err != nil || outputFormat != "yaml" {
		t.Fatalf("OPCUACTL_FORMAT does not win: %q, %v", outputFormat, err)
	}
}
