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
		Use:   "decode [file]",
		Short: "Decode a captured UADP or JSON NetworkMessage",
		Long: `Decode one PubSub NetworkMessage from a file, or from stdin without a file
or with "-".

The input is the raw message (a UDP payload or MQTT message body), or its hex
or base64 text (--input hex|base64; auto detects JSON, hex and raw binary).
The mapping is detected (--encoding forces uadp or json).

The message may be a ua-data message (printed as one line per
DataSetMessage), a ua-metadata message, a ua-status message or a UADP
DataSetMetaData announcement.

UADP fields are named and RawData fields typed with the DataSetMetaData of
--field, --metadata or --metadata-node. Secured messages need the keys of
their SecurityGroup (--keys-file, or --security-group with the SKS at
--endpoint).

Examples:
  opcuactl pubsub decode capture.bin
  opcuactl pubsub decode capture.bin --field Counter:UInt32 --field Temp:Double
  echo 'f1 ... ' | opcuactl pubsub decode --input hex
  opcuactl pubsub decode message.json --format json --pretty
  opcuactl pubsub decode secured.bin --keys-file keys.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			enc, err := parse.ParsePubSubEncoding(encoding, true)
			if err != nil {
				return exitCode(1, err)
			}
			minMode, err := parse.MessageSecurity(sf.mode)
			if err != nil {
				return exitCode(1, err)
			}

			var raw []byte
			if len(args) == 0 || args[0] == "-" {
				raw, err = io.ReadAll(os.Stdin)
			} else {
				raw, err = os.ReadFile(args[0])
			}
			if err != nil {
				return exitCode(1, err)
			}
			payload, err := decodeInput(raw, input)
			if err != nil {
				return exitCode(1, err)
			}
			if len(payload) == 0 {
				return exitCodef(1, "empty message")
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
			res, err := dec.decode(payload, nil)
			if errors.Is(err, errFiltered) {
				if !quiet {
					format.Errf("the message does not match the filters")
				}
				return nil
			}
			if err != nil {
				return exitCodef(3, "decoding the message: %v", err)
			}

			w := newWriter()
			table := format.ParseFormat(outputFormat) == format.Table
			switch {
			case len(res.MetaData) > 0:
				if table {
					fmt.Println(metaDataLine(res.MetaData[0]))
					return nil
				}
				return w.Write(res.MetaData[0])
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
