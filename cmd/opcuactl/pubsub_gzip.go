// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/transport"
	mqtttransport "github.com/otfabric/go-opcua/pubsub/transport/mqtt"
	"github.com/otfabric/opcuactl/internal/parse"
)

var pubsubJSONCompressionValues = []string{mqtttransport.CompressionNone, mqtttransport.CompressionGzip}

// checkJSONCompression validates --json-compression: gzip needs JSON over
// an MQTT broker (Part 14 §7.3.4.9.1 defines application/json+gzip for
// MQTT 5.0 only). It reports whether messages are to be compressed.
func checkJSONCompression(v string, enc parse.PubSubEncoding, url string) (bool, error) {
	switch v {
	case "", mqtttransport.CompressionNone:
		return false, nil
	case mqtttransport.CompressionGzip:
	default:
		return false, exitCodef(exitUsage, "--json-compression must be %s", strings.Join(pubsubJSONCompressionValues, " or "))
	}
	if scheme, _ := transport.Scheme(url); !isMQTTScheme(scheme) || enc != parse.EncodingJSON {
		return false, exitCodef(exitUsage, "--json-compression gzip needs --encoding json and an MQTT broker (mqtt://, mqtts://)")
	}
	return true, nil
}

// mqttVersionOf returns the MQTT version a connection runs, or "" for
// other transports.
func mqttVersionOf(c transport.Connection) string {
	if v, ok := c.(interface{ Version() string }); ok {
		return v.Version()
	}
	return ""
}

// gzipJSON compresses a JSON NetworkMessage.
func gzipJSON(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b) // writes to a bytes.Buffer do not fail
	_ = zw.Close()
	return buf.Bytes()
}

// gunzipJSON returns the JSON NetworkMessage of a gzip compressed message:
// one with the content type application/json+gzip or, without a content
// type (MQTT 3.1.1), one that starts like a gzip stream. ok is false for
// any other message. At most pubsub.DefaultJSONDecompressionLimit bytes are
// decompressed.
func gunzipJSON(b []byte, msg *transport.Message) (plain []byte, ok bool, err error) {
	ct := ""
	if msg != nil {
		ct, _, _ = strings.Cut(msg.ContentType, ";")
		ct = strings.ToLower(strings.TrimSpace(ct))
	}
	switch {
	case ct == transport.ContentTypeJSONGzip:
	case ct == "" && len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b:
	default:
		return nil, false, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, true, fmt.Errorf("gzip compressed JSON: %w", err)
	}
	const limit = pubsub.DefaultJSONDecompressionLimit
	plain, err = io.ReadAll(io.LimitReader(zr, limit+1))
	if err != nil {
		return nil, true, fmt.Errorf("gzip compressed JSON: %w", err)
	}
	if len(plain) > limit {
		return nil, true, fmt.Errorf("gzip compressed JSON: more than %d bytes decompressed", limit)
	}
	return plain, true, nil
}
