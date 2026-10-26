// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

var pubsubInputValues = []string{"auto", "raw", "hex", "base64"}

func newPubSubDecodeCmd() *cobra.Command {
	var (
		sf       pubsubSecurityFlags
		mf       pubsubMetaFlags
		ff       pubsubFilterFlags
		encoding string
		input    string
	)

	cmd := &cobra.Command{
		Use:   "decode [file...]",
		Short: "Decode captured UADP or JSON NetworkMessages",
		Long: `Decode PubSub NetworkMessages from files, or one from stdin without a file
or with "-".

The input is the raw message (a UDP payload or MQTT message body), or its hex
or base64 text (--input hex|base64; auto detects JSON, hex and raw binary).
The mapping is detected (--encoding forces uadp or json).

A message may be a ua-data message (printed as one line per
DataSetMessage), a ua-metadata message, a ua-status message, a UADP
DataSetMetaData or other discovery announcement. The chunks of a chunked UADP
message, given as several files, are reassembled; a DataSetMetaData
announcement before the data names its fields.

UADP fields are named and RawData fields typed with the DataSetMetaData of
--field, --metadata or --metadata-node. Secured messages need the keys of
their SecurityGroup (--keys-file, or --security-group with the SKS at
--endpoint).

Examples:
  opcuactl pubsub decode capture.bin
  opcuactl pubsub decode capture.bin --field Counter:UInt32 --field Temp:Double
  opcuactl pubsub decode chunk-1.bin chunk-2.bin chunk-3.bin
  echo 'f1 ... ' | opcuactl pubsub decode --input hex
  opcuactl pubsub decode message.json --format json --pretty
  opcuactl pubsub decode secured.bin --keys-file keys.json`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := parse.ParsePubSubEncoding(encoding, true)
			if err != nil {
				return exitCode(1, err)
			}
			minMode, err := parse.MessageSecurity(sf.mode)
			if err != nil {
				return exitCode(1, err)
			}
			if len(args) == 0 {
				args = []string{"-"}
			}
			payloads := make([][]byte, len(args))
			for i, name := range args {
				var raw []byte
				if name == "-" {
					raw, err = io.ReadAll(os.Stdin)
				} else {
					raw, err = os.ReadFile(name)
				}
				if err != nil {
					return exitCode(1, err)
				}
				if payloads[i], err = decodeInput(raw, input); err != nil {
					return exitCode(1, err)
				}
				if len(payloads[i]) == 0 {
					return exitCodef(1, "%s: empty message", name)
				}
			}

			ctx := context.Background()
			md, err := mf.load(ctx)
			if err != nil {
				return err
			}
			keys, closeKeys, err := sf.keySource(ctx)
			if err != nil {
				return err
			}
			defer closeKeys()

			dec, err := newPubSubDecoder(enc, ff, md, keys, minMode)
			if err != nil {
				return exitCode(1, err)
			}
			shown := 0
			for i, payload := range payloads {
				res, err := dec.decode(payload, nil)
				if errors.Is(err, errFiltered) {
					continue
				}
				if err != nil {
					if len(payloads) > 1 {
						return exitCodef(3, "decoding %s: %v", args[i], err)
					}
					return exitCodef(3, "decoding the message: %v", err)
				}
				shown++
				if err := writeDecoded(res); err != nil {
					return err
				}
			}
			if shown == 0 && !quiet {
				if st := dec.reasm.Stats(); st.Pending > 0 {
					format.Infof("incomplete chunked message: chunks are missing")
				} else {
					format.Infof("the message does not match the filters")
				}
			}
			return nil
		},
	}

	fs := cmd.Flags()
	sf.register(cmd, "Minimum security of the message (none|sign|sign-and-encrypt)")
	mf.register(cmd)
	ff.register(fs)
	fs.StringVar(&encoding, "encoding", "auto", "Message mapping (auto|uadp|json)")
	fs.StringVar(&input, "input", "auto", "Input form (auto|raw|hex|base64)")
	_ = cmd.RegisterFlagCompletionFunc("encoding", fixedCompletion(pubsubEncodingValues))
	_ = cmd.RegisterFlagCompletionFunc("input", fixedCompletion(pubsubInputValues))
	return cmd
}

// writeDecoded prints what a NetworkMessage held.
func writeDecoded(res *pubsubResult) error {
	w := newWriter()
	table := format.ParseFormat(outputFormat) == format.Table
	switch {
	case len(res.MetaData) > 0:
		if table {
			fmt.Println(metaDataLine(res.MetaData[0]))
			return nil
		}
		return w.Write(res.MetaData[0])
	case len(res.Announcements) > 0:
		if table {
			fmt.Println(announcementLine(res.Announcements[0]))
			return nil
		}
		return w.Write(res.Announcements[0])
	case res.Status != nil:
		if table {
			fmt.Println(statusLine(res.Status))
			return nil
		}
		return w.Write(res.Status)
	}
	if table {
		for _, ds := range res.DataSets {
			fmt.Println(dataSetLine(ds))
		}
		return nil
	}
	items := make([]interface{}, len(res.DataSets))
	for i := range res.DataSets {
		items[i] = res.DataSets[i]
	}
	return w.Write(items)
}

// decodeInput turns the input of `pubsub decode` into the message bytes.
func decodeInput(raw []byte, form string) ([]byte, error) {
	switch strings.ToLower(form) {
	case "raw":
		return raw, nil
	case "hex":
		return decodeHex(raw)
	case "base64":
		return base64.StdEncoding.DecodeString(string(bytes.Join(bytes.Fields(raw), nil)))
	case "", "auto":
		if detectEncoding(raw) == parse.EncodingJSON {
			return raw, nil
		}
		if b, err := decodeHex(raw); err == nil && len(b) > 0 {
			return b, nil
		}
		return raw, nil
	}
	return nil, fmt.Errorf("unknown --input %q (auto|raw|hex|base64)", form)
}

// decodeHex reads hex text, ignoring white space, colons and a 0x prefix.
func decodeHex(raw []byte) ([]byte, error) {
	s := string(bytes.Join(bytes.Fields(raw), nil))
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	s = strings.ReplaceAll(s, ":", "")
	return hex.DecodeString(s)
}
