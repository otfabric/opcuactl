// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/otfabric/go-opcua/pubsub"
	"github.com/otfabric/go-opcua/pubsub/message"
	"github.com/otfabric/go-opcua/pubsub/transport"
	"github.com/otfabric/go-opcua/pubsub/uadp"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

var pubsubProbeValues = []string{"applications", "endpoints", "metadata", "writers", "group", "connections"}

// discoveryProbes builds the probes of `pubsub discover`.
func discoveryProbes(kinds []string, pub *message.PublisherID, writerIDs []uint16, groupID uint16,
	includeWriters, includeGroups bool, profiles []string, layout message.DiscoveryLayout) ([]*message.NetworkMessage, error) {
	var out []*message.NetworkMessage
	var p message.PublisherID
	if pub != nil {
		p = *pub
	}
	for _, k := range kinds {
		req := message.DiscoveryRequest{ProbeType: message.ProbePublisherInformation, Layout: layout}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "applications", "apps":
			req.ProbeType = message.ProbeFindApplications
		case "endpoints":
			req.InformationType = message.InformationPublisherEndpoints
		case "metadata":
			req.InformationType, req.DataSetWriterIDs = message.InformationDataSetMetaData, writerIDs
		case "writers":
			req.InformationType, req.DataSetWriterIDs = message.InformationDataSetWriterConfig, writerIDs
		case "group":
			if groupID == 0 {
				return nil, errors.New("--probe group needs --writer-group-id")
			}
			req.InformationType, req.WriterGroupID, req.IncludeDataSetWriters = message.InformationWriterGroupConfig, groupID, includeWriters
		case "connections":
			req.InformationType = message.InformationPubSubConnections
			req.TransportProfileURIs, req.IncludeWriterGroups, req.IncludeDataSetWriters = profiles, includeGroups, includeWriters
		default:
			return nil, fmt.Errorf("unknown probe %q (%s)", k, strings.Join(pubsubProbeValues, "|"))
		}
		out = append(out, probeMessage(p, pub != nil, req))
	}
	if len(out) == 0 {
		return nil, errors.New("--probe is empty")
	}
	return out, nil
}

func newPubSubDiscoverCmd() *cobra.Command {
	var (
		sf             pubsubSecurityFlags
		gf             pubsubDiagnosticsFlags
		url, iface     string
		listen         []string
		kinds          []string
		publisherID    string
		writerIDs      []uint
		groupID        uint16
		includeWriters bool
		includeGroups  bool
		profiles       []string
		layout         string
		duration       time.Duration
		count          int
		jsonl          bool
	)
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Send UADP discovery probes and print the announcements",
		Long: `Send UADP discovery probes (Part 14 §7.2.4.6) to --url and print the
announcements publishers send back for --duration.

--url is a multicast group (opc.udp://224.0.0.22:4840), an Ethernet address
(opc.eth://, with --interface) or the unicast DiscoveryAddress of a
publisher. Publishers send the DataSetMetaData and DataSetWriter
configuration announcements to the address of their data, the others to
their DiscoveryAddress: --listen-url adds addresses to listen on, and is
required with a unicast --url (which only takes probes).

--probe selects what to ask for (repeatable or comma separated):
  applications  FindApplications: every publisher answers with its
                ApplicationDescription or its state (default)
  endpoints     the server endpoints of the publishers
  metadata      the DataSetMetaData of --writer-id (all writers without)
  writers       the DataSetWriter configuration of --writer-id
  group         the configuration of WriterGroup --writer-group-id
                (--include-writers adds its DataSetWriters)
  connections   the PubSubConnections, of --transport-profile only if given
                (--include-groups and --include-writers add groups and writers)
--publisher-id addresses the probes to one publisher; --discovery-layout
dotnet sends them in the layout of the OPC Foundation .NET stack 2.x.

Examples:
  opcuactl pubsub discover --url opc.udp://224.0.0.22:4840
  opcuactl pubsub discover --url opc.udp://224.0.0.22:4840 --probe metadata,writers --publisher-id 2234 --writer-id 1
  opcuactl pubsub discover --url opc.udp://224.0.0.23:4840 --listen-url opc.udp://224.0.0.22:4840 --probe connections --include-groups
  sudo opcuactl pubsub discover --url opc.eth://01-00-5E-00-00-01 --interface eth0 --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := gf.validate(); err != nil {
				return err
			}
			lay, err := parse.DiscoveryLayout(layout)
			if err != nil {
				return exitCode(1, err)
			}
			tf := pubsubTransportFlags{url: url, iface: iface}
			if _, err := tf.config(parse.EncodingUADP); err != nil {
				return err
			}
			for _, u := range append([]string{url}, listen...) {
				if s, _ := transport.Scheme(u); s != "opc.udp" && s != "opc.eth" {
					return exitCodef(1, "%s: UADP discovery needs opc.udp:// or opc.eth://", u)
				}
			}
			if !sharedAddress(url) && len(listen) == 0 {
				return exitCodef(1, "%s is a unicast address: give --listen-url for the announcements (the address of the publisher's data)", url)
			}
			var pub *message.PublisherID
			if publisherID != "" {
				p, err := parse.PublisherID(publisherID)
				if err != nil {
					return exitCode(1, err)
				}
				pub = &p
			}
			ids := make([]uint16, len(writerIDs))
			for i, id := range writerIDs {
				if id == 0 || id > 0xffff {
					return exitCodef(1, "--writer-id %d is not a DataSetWriterId (1-65535)", id)
				}
				ids[i] = uint16(id)
			}
			probes, err := discoveryProbes(kinds, pub, ids, groupID, includeWriters, includeGroups, profiles, lay)
			if err != nil {
				return exitCode(1, err)
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			keys, closeKeys, err := sf.keySource(ctx)
			if err != nil {
				return err
			}
			defer closeKeys()
			minMode, err := parse.MessageSecurity(sf.mode)
			if err != nil {
				return exitCode(1, err)
			}
			dec, err := newPubSubDecoder(parse.EncodingUADP, pubsubFilterFlags{publisherID: publisherID}, nil, keys, minMode)
			if err != nil {
				return exitCode(1, err)
			}
			dec.diag = newPubSubDiagnostics("discovery", []pubsub.DiagnosticsCounterID{pubsub.CounterReceivedNetworkMessages,
				pubsub.CounterReceivedInvalidNetworkMessages, pubsub.CounterDecryptionErrors},
				ctrChunksReceived, ctrChunkedMessagesReceived, ctrDiscoveryRequestsSent, ctrDiscoveryResponsesReceived)
			defer gf.print(dec.diag)

			msgs := make(chan *transport.Message, 256)
			deliver := func(m *transport.Message) {
				select {
				case msgs <- m:
				default:
				}
			}
			// A shared --url (multicast, Ethernet) carries the answers too;
			// a unicast one is the DiscoveryAddress of a publisher, which
			// only takes probes.
			var conn transport.Connection
			for i, u := range append([]string{url}, listen...) {
				t := pubsubTransportFlags{url: u, iface: iface}
				c, err := t.open(ctx, parse.EncodingUADP)
				if err != nil {
					return err
				}
				defer func() { _ = c.Close() }()
				if i == 0 {
					conn = c
					if !sharedAddress(u) {
						continue
					}
				}
				cancelSub, err := t.subscribe(ctx, c, "", deliver)
				if err != nil {
					return err
				}
				defer func() { _ = cancelSub() }()
			}
			for _, nm := range probes {
				b, err := uadp.Encode(nm, nil)
				if err != nil {
					return exitCodef(3, "encoding the %s probe: %v", nm.Request.InformationType, err)
				}
				sendCtx, cancel := connCfg.TimeoutContext(ctx)
				err = conn.Send(sendCtx, b, transport.PublishOptions{})
				cancel()
				if err != nil {
					return exitCodef(2, "sending a probe to %s: %v", url, transportError(url, err))
				}
				dec.diag.add(ctrDiscoveryRequestsSent, 1)
			}
			if verbose {
				pubsubInfo("sent %d probe(s) to %s, listening for %s", len(probes), url, duration)
			}

			useJSON := jsonl || format.ParseFormat(outputFormat) != format.Table
			emit := func(v interface{}, line string) {
				if useJSON {
					b, _ := json.Marshal(v)
					fmt.Println(string(b))
				} else {
					fmt.Println(line)
				}
			}
			timer := time.NewTimer(duration)
			defer timer.Stop()
			seen := 0
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-timer.C:
					if seen == 0 && !quiet {
						pubsubInfo("no announcements within %s", duration)
					}
					return nil
				case m := <-msgs:
					res, err := dec.decode(m.Payload, m)
					if err != nil {
						if !errors.Is(err, errFiltered) && verbose {
							format.Errf("decoding a %d-byte message: %v", len(m.Payload), err)
						}
						continue
					}
					for _, md := range res.MetaData {
						emit(md, metaDataLine(md))
						seen++
					}
					for _, a := range res.Announcements {
						emit(a, announcementLine(a))
						seen++
					}
					if count > 0 && seen >= count {
						return nil
					}
				}
			}
		},
	}
	fs := cmd.Flags()
	sf.register(cmd, "Minimum security of the announcements (none|sign|sign-and-encrypt)")
	gf.register(fs)
	fs.StringVar(&url, "url", "", "Where to send the probes: a multicast group or DiscoveryAddress (opc.udp://), or opc.eth://")
	fs.StringVar(&iface, "interface", "", "Network interface (UDP multicast; required for opc.eth://)")
	fs.StringArrayVar(&listen, "listen-url", nil, "Further address to listen on for announcements (repeatable)")
	fs.StringSliceVar(&kinds, "probe", []string{"applications"}, "What to probe for ("+strings.Join(pubsubProbeValues, "|")+"; repeatable)")
	fs.StringVar(&publisherID, "publisher-id", "", "Only probe (and show) this PublisherId")
	fs.UintSliceVar(&writerIDs, "writer-id", nil, "DataSetWriterIds of the metadata and writers probes (default: all)")
	fs.Uint16Var(&groupID, "writer-group-id", 0, "WriterGroupId of the group probe")
	fs.BoolVar(&includeWriters, "include-writers", false, "group and connections probes: include the DataSetWriters")
	fs.BoolVar(&includeGroups, "include-groups", false, "connections probe: include the WriterGroups")
	fs.StringArrayVar(&profiles, "transport-profile", nil, "connections probe: only connections with this TransportProfileUri (repeatable)")
	fs.StringVar(&layout, "discovery-layout", "spec", "Byte layout of the probes (spec|dotnet)")
	fs.DurationVar(&duration, "duration", 3*time.Second, "How long to wait for announcements")
	fs.IntVar(&count, "count", 0, "Stop after this many announcements (0 = wait --duration)")
	fs.BoolVar(&jsonl, "jsonl", false, "Output newline-delimited JSON (JSONL)")
	_ = cmd.RegisterFlagCompletionFunc("probe", fixedCompletion(pubsubProbeValues))
	_ = cmd.RegisterFlagCompletionFunc("discovery-layout", fixedCompletion(pubsubDiscoveryLayoutValues))
	_ = cmd.RegisterFlagCompletionFunc("diagnostics-level", fixedCompletion(pubsubDiagnosticsLevelValues))
	return cmd
}
