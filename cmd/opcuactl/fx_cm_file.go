// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
)

// File open modes (OPC 10000-20 §4.2.2 Table 4); ConnectionManagerConfiguration
// allows Read and Write with EraseExisting only (Part 81 F.3.2).
const (
	cmFileRead       = 0x01
	cmFileWriteErase = 0x02 | 0x04
)

// cmCleanup bounds the calls that undo a step after a failure (Close of a
// file, DiscardUpdates of an edit). They run on a context of their own, so
// that they are sent when the command's context ran out or was cancelled.
var cmCleanup = 10 * time.Second

// cleanupContext is a context for an undo after a failure on ctx.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cmCleanup)
}

// cmFile is the ConnectionManagerConfiguration file of a ConnectionManager
// (Part 81 F.3) and its Methods, found by BrowseName.
type cmFile struct {
	x                                        *cmClient
	obj                                      *ua.NodeID
	open, read, write, close, closeAndUpdate *ua.NodeID
}

// file finds ConnectionManagerConfiguration (F.3, an optional member of
// ConnectionManagerType).
func (x *cmClient) file(ctx context.Context) (*cmFile, error) {
	obj, err := x.child(ctx, x.cm, x.ns.CM, "ConnectionManagerConfiguration")
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, exitCodef(exitUnsupported, "the ConnectionManager has no ConnectionManagerConfiguration file (Part 81 F.3)")
	}
	f := &cmFile{x: x, obj: obj}
	for _, m := range []struct {
		ns   uint16
		name string
		dst  **ua.NodeID
	}{
		{0, "Open", &f.open}, {0, "Read", &f.read}, {0, "Write", &f.write}, {0, "Close", &f.close},
		{x.ns.CM, "CloseAndUpdate", &f.closeAndUpdate},
	} {
		n, err := x.child(ctx, obj, m.ns, m.name)
		if err != nil {
			return nil, err
		}
		*m.dst = n
	}
	if f.open == nil || f.close == nil {
		return nil, fmt.Errorf("ConnectionManagerConfiguration has no Open or Close Method (OPC 10000-20 §4.2)")
	}
	return f, nil
}

func (f *cmFile) call(ctx context.Context, name string, method *ua.NodeID, args ...*ua.Variant) ([]*ua.Variant, error) {
	if method == nil {
		return nil, exitCodef(exitUnsupported, "ConnectionManagerConfiguration has no %s Method", name)
	}
	res, err := f.x.c.Call(ctx, &ua.CallMethodRequest{ObjectID: f.obj, MethodID: method, InputArguments: args})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if res.StatusCode.Uint32()&0x80000000 != 0 {
		return res.OutputArguments, fmt.Errorf("%s: %w", name, res.StatusCode)
	}
	return res.OutputArguments, nil
}

// closeAfter closes handle h after a failure on ctx.
func (f *cmFile) closeAfter(ctx context.Context, h uint32) {
	cctx, cancel := cleanupContext(ctx)
	defer cancel()
	_, _ = f.call(cctx, "Close", f.close, ua.MustVariant(h))
}

func (f *cmFile) openFile(ctx context.Context, mode uint8) (uint32, error) {
	out, err := f.call(ctx, "Open", f.open, ua.MustVariant(mode))
	if err != nil {
		return 0, err
	}
	if len(out) != 1 || out[0] == nil {
		return 0, fmt.Errorf("the Open Method returned %d output arguments, want 1", len(out))
	}
	h, ok := out[0].Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("the FileHandle of Open is a %T, not a UInt32", out[0].Value())
	}
	return h, nil
}

// readAll reads the file: the sets of the ConnectionManager as the file of
// F.2. A Read that returns an empty ByteString ends the file; one that
// returns anything but one ByteString is a protocol error.
func (f *cmFile) readAll(ctx context.Context) ([]byte, error) {
	h, err := f.openFile(ctx, cmFileRead)
	if err != nil {
		return nil, err
	}
	var data []byte
	for {
		out, err := f.call(ctx, "Read", f.read, ua.MustVariant(h), ua.MustVariant(int32(cmChunk)))
		if err != nil {
			f.closeAfter(ctx, h)
			return nil, err
		}
		b, err := readChunk(out)
		if err != nil {
			f.closeAfter(ctx, h)
			return nil, err
		}
		if len(b) == 0 {
			break
		}
		if len(b) > cmChunk {
			f.closeAfter(ctx, h)
			return nil, fmt.Errorf("the Read Method returned %d bytes for a Length of %d", len(b), cmChunk)
		}
		if len(data)+len(b) > cmMaxFileSize {
			f.closeAfter(ctx, h)
			return nil, exitCodef(exitPartial, "the file is larger than %d bytes", cmMaxFileSize)
		}
		data = append(data, b...)
	}
	if _, err := f.call(ctx, "Close", f.close, ua.MustVariant(h)); err != nil {
		return nil, err
	}
	return data, nil
}

// readChunk is the Data of a Read (OPC 10000-20 §4.2.4): one ByteString.
// An array of Byte is taken as the same bytes (Part 6 §5.2.2.7).
func readChunk(out []*ua.Variant) ([]byte, error) {
	if len(out) != 1 || out[0] == nil {
		return nil, fmt.Errorf("the Read Method returned %d output arguments, want 1", len(out))
	}
	switch v := out[0].Value().(type) {
	case []byte:
		return v, nil
	case ua.ByteArray:
		return v, nil
	default:
		return nil, fmt.Errorf("the Data of Read is a %T, not a ByteString", v)
	}
}

// cmUpdate is the answer of CloseAndUpdate (F.3.3, Table F.39).
type cmUpdate struct {
	applied bool
	results []ua.StatusCode
	objs    []*ua.NodeID // nil, or one per operation
}

// update writes data and applies its sets with ops, one per set
// (CloseAndUpdate, F.3.3). CloseAndUpdate is called once: it changes the
// server, so no failure of it is retried.
func (f *cmFile) update(ctx context.Context, data []byte, require bool, ops []uafx.ConnectionConfigurationSetOperation) (cmUpdate, error) {
	if f.write == nil || f.closeAndUpdate == nil {
		return cmUpdate{}, exitCodef(exitUnsupported, "ConnectionManagerConfiguration has no Write or CloseAndUpdate Method")
	}
	h, err := f.openFile(ctx, cmFileWriteErase)
	if err != nil {
		return cmUpdate{}, err
	}
	for off := 0; off < len(data); off += cmChunk {
		end := min(off+cmChunk, len(data))
		if _, err := f.call(ctx, "Write", f.write, ua.MustVariant(h), ua.MustVariant(data[off:end])); err != nil {
			f.closeAfter(ctx, h)
			return cmUpdate{}, err
		}
	}
	raw := make([]byte, len(ops))
	for i, o := range ops {
		raw[i] = byte(o)
	}
	// Operations is an array of ConnectionConfigurationSetOperation, an
	// OptionSet of Byte (Table F.39). It is sent as a ByteString, which a
	// server takes where an array of Byte is expected (OPC 10000-4
	// §5.12.2.4), and the only form go-opcua v1.39.0 accepts.
	out, err := f.call(ctx, "CloseAndUpdate", f.closeAndUpdate, ua.MustVariant(h), ua.MustVariant(require), ua.MustVariant(raw))
	if err != nil {
		return cmUpdate{}, err
	}
	u, err := closeAndUpdateResult(out, len(ops))
	if err != nil {
		return cmUpdate{}, err
	}
	if err := u.check(require); err != nil {
		return u, err
	}
	return u, nil
}

// closeAndUpdateResult checks the output arguments of CloseAndUpdate
// (Table F.39): ChangesApplied, one Result per operation and the
// ConfigurationObjects, empty or one per operation.
func closeAndUpdateResult(out []*ua.Variant, n int) (cmUpdate, error) {
	var u cmUpdate
	if len(out) != 3 || out[0] == nil || out[1] == nil {
		return u, fmt.Errorf("CloseAndUpdate returned %d output arguments, want 3", len(out))
	}
	applied, ok := out[0].Value().(bool)
	if !ok {
		return u, fmt.Errorf("CloseAndUpdate: ChangesApplied is a %T, not a Boolean", out[0].Value())
	}
	u.applied = applied
	if u.results, ok = out[1].Value().([]ua.StatusCode); !ok {
		return u, fmt.Errorf("CloseAndUpdate: Results is a %T, not an array of StatusCode", out[1].Value())
	}
	if len(u.results) != n {
		return u, fmt.Errorf("CloseAndUpdate: %d Results for %d operations", len(u.results), n)
	}
	if out[2] != nil && out[2].Value() != nil {
		if u.objs, ok = out[2].Value().([]*ua.NodeID); !ok {
			return u, fmt.Errorf("CloseAndUpdate: ConfigurationObjects is a %T, not an array of NodeId", out[2].Value())
		}
		if len(u.objs) != 0 && len(u.objs) != n {
			return u, fmt.Errorf("CloseAndUpdate: %d ConfigurationObjects for %d operations", len(u.objs), n)
		}
	}
	return u, nil
}

// check refuses an answer whose ChangesApplied contradicts its Results:
// changes applied with no operation Good, every operation Good and none
// applied, or with RequireCompleteUpdate some applied and some not.
func (u cmUpdate) check(require bool) error {
	good := 0
	for _, r := range u.results {
		if r == ua.StatusOK {
			good++
		}
	}
	switch {
	case u.applied && good == 0:
		return fmt.Errorf("CloseAndUpdate: ChangesApplied is TRUE and no operation succeeded")
	case !u.applied && good == len(u.results) && good > 0:
		return fmt.Errorf("CloseAndUpdate: ChangesApplied is FALSE and every operation succeeded")
	case require && u.applied && good < len(u.results):
		return fmt.Errorf("CloseAndUpdate: with RequireCompleteUpdate ChangesApplied is TRUE and %d of %d operations failed", len(u.results)-good, len(u.results))
	}
	return nil
}
