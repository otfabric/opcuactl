// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uacp"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/spf13/cobra"
)

func newPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Test connectivity to an OPC UA server",
		Long: `Test connectivity to an OPC UA server in stages.

Each stage is tested and reported individually:
  1. TCP    — raw TCP connection to the endpoint
  2. Secure — SecureChannel + Session creation
  3. Browse — read the Root/Objects folder to confirm operability

With --reverse-listen the TCP stage is skipped: the server connects to opcuactl.

Examples:
  opcuactl ping --endpoint opc.tcp://localhost:4840
  opcuactl ping -e opc.tcp://localhost:4840 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := connCfg.RequireEndpoint(); err != nil {
				return exitCode(1, err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var steps []model.PingStep

			// Stage 1: TCP dial (not with reverse connect: the server dials us)
			if !connCfg.Reverse() {
				tcpStart := time.Now()
				conn, err := uacp.DialTCP(ctx, connCfg.Endpoint)
				tcpDur := time.Since(tcpStart)
				if err != nil {
					steps = append(steps, model.PingStep{
						Stage:   "tcp",
						Status:  "fail",
						Latency: tcpDur.String(),
						Error:   err.Error(),
					})
					return outputPing(steps)
				}
				_ = conn.Close()
				steps = append(steps, model.PingStep{
					Stage:   "tcp",
					Status:  "ok",
					Latency: tcpDur.String(),
				})
			}

			// Stage 2: Full connect (SecureChannel + Session)
			secStart := time.Now()
			c, err := connCfg.NewClient(ctx)
			secDur := time.Since(secStart)
			if err != nil {
				steps = append(steps, model.PingStep{
					Stage:   "secure",
					Status:  "fail",
					Latency: secDur.String(),
					Error:   err.Error(),
				})
				return outputPing(steps)
			}
			defer func() { _ = c.Close(ctx) }()
			steps = append(steps, model.PingStep{
				Stage:   "secure",
				Status:  "ok",
				Latency: secDur.String(),
			})

			// Stage 3: Browse root
			browseStart := time.Now()
			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()
			n := c.Node(ua.NewNumericNodeID(0, id.ObjectsFolder))
			_, browseErr := n.Children(opCtx, id.HierarchicalReferences, ua.NodeClassAll)
			browseDur := time.Since(browseStart)
			if browseErr != nil {
				steps = append(steps, model.PingStep{
					Stage:   "browse",
					Status:  "fail",
					Latency: browseDur.String(),
					Error:   browseErr.Error(),
				})
			} else {
				steps = append(steps, model.PingStep{
					Stage:   "browse",
					Status:  "ok",
					Latency: browseDur.String(),
				})
			}

			return outputPing(steps)
		},
	}
}

func outputPing(steps []model.PingStep) error {
	w := newWriter()
	f := format.ParseFormat(outputFormat)
	if f == format.Table {
		for _, s := range steps {
			icon := "✓"
			if s.Status == "fail" {
				icon = "✗"
			}
			if s.Error != "" {
				fmt.Printf("  %s %-8s %s  (%s)\n", icon, s.Stage, s.Latency, s.Error)
			} else {
				fmt.Printf("  %s %-8s %s\n", icon, s.Stage, s.Latency)
			}
		}
		return nil
	}
	return w.Write(steps)
}
