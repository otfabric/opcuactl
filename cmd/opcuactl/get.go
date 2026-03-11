package opcuactl

import (
	"context"
	"fmt"

	"github.com/otfabric/opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Read values, attributes, and server information",
		Long: `Read node values, attributes, node summaries, and server status.

Examples:
  opcuactl get value --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get node --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get server --endpoint opc.tcp://localhost:4840`,
	}
	cmd.AddCommand(newGetValueCmd(), newGetNodeCmd(), newGetServerCmd())
	return cmd
}

func newGetValueCmd() *cobra.Command {
	var (
		nodes      []string
		timestamps bool
	)

	cmd := &cobra.Command{
		Use:   "value",
		Short: "Read node values",
		Long: `Read one or more node values from the server.

Examples:
  opcuactl get value --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get value -e opc.tcp://localhost:4840 -n i=2258 -n "ns=2;s=Temperature" --format json
  opcuactl get value --endpoint opc.tcp://localhost:4840 --node i=2258 --timestamps`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(nodes) == 0 {
				return exitCodef(1, "at least one --node is required")
			}

			nodeIDs, err := parse.NodeIDs(nodes)
			if err != nil {
				return exitCode(1, err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()

			w := newWriter()

			if len(nodeIDs) == 1 {
				dv, err := c.ReadValue(opCtx, nodeIDs[0])
				if err != nil {
					format.Errf("reading value: %v", err)
					return exitCode(3, err)
				}
				return outputValues(w, nodeIDs, []*ua.DataValue{dv}, timestamps)
			}

			dvs, err := c.ReadValues(opCtx, nodeIDs...)
			if err != nil {
				format.Errf("reading values: %v", err)
				return exitCode(3, err)
			}
			return outputValues(w, nodeIDs, dvs, timestamps)
		},
	}

	cmd.Flags().StringArrayVarP(&nodes, "node", "n", nil, "Node ID to read (repeatable)")
	cmd.Flags().BoolVar(&timestamps, "timestamps", false, "Show source and server timestamps")
	return cmd
}

func outputValues(w *format.Writer, nodeIDs []*ua.NodeID, dvs []*ua.DataValue, timestamps bool) error {
	var values []model.NodeValue
	for i, dv := range dvs {
		nv := model.NodeValue{
			NodeID:     nodeIDs[i].String(),
			Status:     dv.Status.Error(),
			StatusCode: uint32(dv.Status),
		}
		if dv.Value != nil {
			nv.Value = dv.Value.Value()
			nv.Type = dv.Value.Type().String()
		}
		if timestamps {
			nv.SourceTimestamp = model.FormatTimestamp(dv.SourceTimestamp)
			nv.ServerTimestamp = model.FormatTimestamp(dv.ServerTimestamp)
		}
		values = append(values, nv)
	}

	f := format.ParseFormat(outputFormat)
	if f == format.Table {
		headers := []string{"NODE ID", "VALUE", "TYPE", "STATUS"}
		if timestamps {
			headers = append(headers, "SOURCE TIMESTAMP", "SERVER TIMESTAMP")
		}
		rows := make([][]string, len(values))
		for i, v := range values {
			row := []string{v.NodeID, fmt.Sprintf("%v", v.Value), v.Type, v.Status}
			if timestamps {
				row = append(row, v.SourceTimestamp, v.ServerTimestamp)
			}
			rows[i] = row
		}
		return w.WriteTable(headers, rows)
	}
	if len(values) == 1 {
		return w.Write(values[0])
	}
	return w.Write(values)
}

func newGetNodeCmd() *cobra.Command {
	var nodes []string

	cmd := &cobra.Command{
		Use:   "node",
		Short: "Get node summary",
		Long: `Read a rich summary of one or more nodes including common attributes.

Examples:
  opcuactl get node --endpoint opc.tcp://localhost:4840 --node i=2258
  opcuactl get node --endpoint opc.tcp://localhost:4840 --node "ns=0;i=85" --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(nodes) == 0 {
				return exitCodef(1, "at least one --node is required")
			}

			nodeIDs, err := parse.NodeIDs(nodes)
			if err != nil {
				return exitCode(1, err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()

			w := newWriter()
			var infos []model.NodeInfo
			for _, nid := range nodeIDs {
				n := c.Node(nid)
				summary, err := n.Summary(opCtx)
				if err != nil {
					format.Errf("getting summary for %s: %v", nid, err)
					return exitCode(3, err)
				}

				info := model.NodeInfo{
					NodeID:    summary.NodeID.String(),
					NodeClass: summary.NodeClass.String(),
				}
				if summary.BrowseName != nil {
					info.BrowseName = summary.BrowseName.Name
				}
				if summary.DisplayName != nil {
					info.DisplayName = summary.DisplayName.Text
				}
				if summary.Description != nil {
					info.Description = summary.Description.Text
				}
				if summary.DataType != nil {
					info.DataType = summary.DataType.String()
				}
				if summary.AccessLevel != 0 {
					info.AccessLevel = summary.AccessLevel.String()
				}
				if summary.UserAccessLevel != 0 {
					info.UserAccessLevel = summary.UserAccessLevel.String()
				}
				if summary.TypeDefinition != nil {
					info.TypeDefinition = summary.TypeDefinition.String()
				}
				if summary.Value != nil && summary.Value.Value != nil {
					info.Value = summary.Value.Value.Value()
					info.ValueStatus = summary.Value.Status.Error()
				}
				infos = append(infos, info)
			}

			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"NODE ID", "CLASS", "BROWSE NAME", "DISPLAY NAME", "DATA TYPE", "VALUE"}
				rows := make([][]string, len(infos))
				for i, info := range infos {
					val := ""
					if info.Value != nil {
						val = fmt.Sprintf("%v", info.Value)
					}
					rows[i] = []string{info.NodeID, info.NodeClass, info.BrowseName, info.DisplayName, info.DataType, val}
				}
				return w.WriteTable(headers, rows)
			}
			if len(infos) == 1 {
				return w.Write(infos[0])
			}
			return w.Write(infos)
		},
	}

	cmd.Flags().StringArrayVarP(&nodes, "node", "n", nil, "Node ID (repeatable)")
	return cmd
}

func newGetServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Get server status",
		Long: `Read server status, build info, and connection security details.

Examples:
  opcuactl get server --endpoint opc.tcp://localhost:4840
  opcuactl get server --endpoint opc.tcp://localhost:4840 --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			opCtx, opCancel := connCfg.TimeoutContext(ctx)
			defer opCancel()

			ss, err := c.ServerStatus(opCtx)
			if err != nil {
				format.Errf("getting server status: %v", err)
				return exitCode(3, err)
			}

			w := newWriter()
			status := model.ServerStatus{
				StartTime:           model.FormatTimestamp(ss.StartTime),
				CurrentTime:         model.FormatTimestamp(ss.CurrentTime),
				State:               ss.State.String(),
				SecondsTillShutdown: ss.SecondsTillShutdown,
				SecurityPolicy:      parse.SecurityPolicyShortName(c.SecurityPolicy()),
				SecurityMode:        c.SecurityMode().String(),
			}
			if ss.ShutdownReason != nil {
				status.ShutdownReason = ss.ShutdownReason.Text
			}
			if ss.BuildInfo != nil {
				status.BuildInfo = &model.BuildInfo{
					ProductURI:       ss.BuildInfo.ProductURI,
					ManufacturerName: ss.BuildInfo.ManufacturerName,
					ProductName:      ss.BuildInfo.ProductName,
					SoftwareVersion:  ss.BuildInfo.SoftwareVersion,
					BuildNumber:      ss.BuildInfo.BuildNumber,
					BuildDate:        model.FormatTimestamp(ss.BuildInfo.BuildDate),
				}
			}

			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				fmt.Printf("Server Status\n")
				fmt.Printf("  State:          %s\n", status.State)
				fmt.Printf("  Start Time:     %s\n", status.StartTime)
				fmt.Printf("  Current Time:   %s\n", status.CurrentTime)
				fmt.Printf("  Security:       %s / %s\n", status.SecurityPolicy, status.SecurityMode)
				if status.BuildInfo != nil {
					fmt.Printf("  Product:        %s %s\n", status.BuildInfo.ProductName, status.BuildInfo.SoftwareVersion)
					fmt.Printf("  Manufacturer:   %s\n", status.BuildInfo.ManufacturerName)
					fmt.Printf("  Build:          %s (%s)\n", status.BuildInfo.BuildNumber, status.BuildInfo.BuildDate)
				}
				return nil
			}
			return w.Write(status)
		},
	}
}
