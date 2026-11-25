// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
)

// File open modes (OPC 10000-20 §4.2.2 Table 4); ConnectionManagerConfiguration
// allows Read and Write with EraseExisting only (Part 81 F.3.2).
const (
	cmFileRead       = 0x01
	cmFileWriteErase = 0x02 | 0x04
)

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
	if res.StatusCode != ua.StatusOK {
		return res.OutputArguments, fmt.Errorf("%s: %w", name, res.StatusCode)
	}
	return res.OutputArguments, nil
}

func (f *cmFile) openFile(ctx context.Context, mode uint8) (uint32, error) {
	out, err := f.call(ctx, "Open", f.open, ua.MustVariant(mode))
	if err != nil {
		return 0, err
	}
	if len(out) != 1 || out[0] == nil {
		return 0, fmt.Errorf("the Open Method returned %d output arguments", len(out))
	}
	h, ok := out[0].Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("the FileHandle of Open is a %T", out[0].Value())
	}
	return h, nil
}

// readAll reads the file: the sets of the ConnectionManager as the file of
// F.2.
func (f *cmFile) readAll(ctx context.Context) ([]byte, error) {
	h, err := f.openFile(ctx, cmFileRead)
	if err != nil {
		return nil, err
	}
	var data []byte
	for {
		out, err := f.call(ctx, "Read", f.read, ua.MustVariant(h), ua.MustVariant(int32(cmChunk)))
		if err != nil {
			_, _ = f.call(ctx, "Close", f.close, ua.MustVariant(h))
			return nil, err
		}
		var b []byte
		if len(out) == 1 && out[0] != nil {
			switch v := out[0].Value().(type) {
			case []byte:
				b = v
			case ua.ByteArray:
				b = v
			}
		}
		if len(b) == 0 {
			break
		}
		if len(data)+len(b) > cmMaxFileSize {
			_, _ = f.call(ctx, "Close", f.close, ua.MustVariant(h))
			return nil, exitCodef(exitPartial, "the file is larger than %d bytes", cmMaxFileSize)
		}
		data = append(data, b...)
	}
	if _, err := f.call(ctx, "Close", f.close, ua.MustVariant(h)); err != nil {
		return nil, err
	}
	return data, nil
}

// update writes data and applies its sets with ops, one per set
// (CloseAndUpdate, F.3.3).
func (f *cmFile) update(ctx context.Context, data []byte, require bool, ops []uafx.ConnectionConfigurationSetOperation) (bool, []ua.StatusCode, []*ua.NodeID, error) {
	if f.write == nil || f.closeAndUpdate == nil {
		return false, nil, nil, exitCodef(exitUnsupported, "ConnectionManagerConfiguration has no Write or CloseAndUpdate Method")
	}
	h, err := f.openFile(ctx, cmFileWriteErase)
	if err != nil {
		return false, nil, nil, err
	}
	for off := 0; off < len(data); off += cmChunk {
		end := min(off+cmChunk, len(data))
		if _, err := f.call(ctx, "Write", f.write, ua.MustVariant(h), ua.MustVariant(data[off:end])); err != nil {
			_, _ = f.call(ctx, "Close", f.close, ua.MustVariant(h))
			return false, nil, nil, err
		}
	}
	raw := make([]byte, len(ops))
	for i, o := range ops {
		raw[i] = byte(o)
	}
	// Operations is an array of ConnectionConfigurationSetOperation, an
	// OptionSet of Byte (Table F.39): an array of Byte. A server that takes
	// it as a ByteString only answers Bad_TypeMismatch before it uses the
	// handle; it gets the ByteString then.
	out, err := f.call(ctx, "CloseAndUpdate", f.closeAndUpdate, ua.MustVariant(h), ua.MustVariant(require), ua.MustVariant(ua.ByteArray(raw)))
	if errors.Is(err, ua.StatusBadTypeMismatch) {
		// A file the server cannot decode is Bad_TypeMismatch too, and that
		// one closed the handle: then the retry finds no handle, and the
		// first answer is the one to report.
		first := err
		out, err = f.call(ctx, "CloseAndUpdate", f.closeAndUpdate, ua.MustVariant(h), ua.MustVariant(require), ua.MustVariant(raw))
		if errors.Is(err, ua.StatusBadInvalidArgument) {
			err = first
		}
	}
	if err != nil {
		return false, nil, nil, err
	}
	if len(out) != 3 || out[0] == nil || out[1] == nil {
		return false, nil, nil, fmt.Errorf("CloseAndUpdate: %d output arguments", len(out))
	}
	applied, _ := out[0].Value().(bool)
	results, _ := out[1].Value().([]ua.StatusCode)
	var objs []*ua.NodeID
	if out[2] != nil {
		objs, _ = out[2].Value().([]*ua.NodeID)
	}
	if len(results) != len(ops) {
		return false, nil, nil, fmt.Errorf("CloseAndUpdate: %d results for %d operations", len(results), len(ops))
	}
	return applied, results, objs, nil
}
