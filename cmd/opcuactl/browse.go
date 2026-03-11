package opcuactl

import (
	"context"

	"github.com/otfabric/opcua/id"
	"github.com/otfabric/opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newBrowseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browse",
		Short: "Browse the OPC UA address space",
		Long: `Browse nodes, children, and references in the OPC UA address space.

Examples:
  opcuactl browse children --endpoint opc.tcp://localhost:4840
  opcuactl browse refs --endpoint opc.tcp://localhost:4840 --node i=85`,
	}
	cmd.AddCommand(newBrowseChildrenCmd(), newBrowseRefsCmd())
	return cmd
}

func newBrowseChildrenCmd() *cobra.Command {
	var (
		nodeStr   string
		nodeClass string
	)

	cmd := &cobra.Command{
		Use:   "children",
		Short: "List child nodes",
		Long: `List direct child nodes of a node.

Examples:
  opcuactl browse children --endpoint opc.tcp://localhost:4840
  opcuactl browse children --endpoint opc.tcp://localhost:4840 --node "ns=0;i=85"
  opcuactl browse children -e opc.tcp://localhost:4840 -n i=2253 --node-class variable --format json`,
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

			nodeID := ua.NewNumericNodeID(0, id.ObjectsFolder)
			if nodeStr != "" {
				nodeID, err = parse.NodeID(nodeStr)
				if err != nil {
					return exitCode(1, err)
				}
			}

			classMask := parse.NodeClassMask(nodeClass)
			n := c.Node(nodeID)
			children, err := n.Children(opCtx, id.HierarchicalReferences, classMask)
			if err != nil {
				format.Errf("browsing children of %s: %v", nodeID, err)
				return exitCode(3, err)
			}

			var results []model.ChildNode
			for _, child := range children {
				summary, err := child.Summary(opCtx)
				if err != nil {
					results = append(results, model.ChildNode{NodeID: child.ID.String()})
					continue
				}
				cn := model.ChildNode{
					NodeID:    summary.NodeID.String(),
					NodeClass: summary.NodeClass.String(),
				}
				if summary.BrowseName != nil {
					cn.BrowseName = summary.BrowseName.Name
				}
				if summary.DisplayName != nil {
					cn.DisplayName = summary.DisplayName.Text
				}
				if summary.TypeDefinition != nil {
					cn.TypeDefinition = summary.TypeDefinition.String()
				}
				results = append(results, cn)
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"NODE ID", "CLASS", "BROWSE NAME", "DISPLAY NAME", "TYPE DEFINITION"}
				rows := make([][]string, len(results))
				for i, r := range results {
					rows[i] = []string{r.NodeID, r.NodeClass, r.BrowseName, r.DisplayName, r.TypeDefinition}
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(results)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Start node ID (default: Objects folder ns=0;i=85)")
	cmd.Flags().StringVar(&nodeClass, "node-class", "all", "Filter by node class (all|object|variable|method|objecttype|variabletype|referencetype|datatype|view)")
	return cmd
}

func newBrowseRefsCmd() *cobra.Command {
	var (
		nodeStr         string
		direction       string
		refType         string
		includeSubtypes bool
		nodeClass       string
	)

	cmd := &cobra.Command{
		Use:   "refs",
		Short: "Inspect node references",
		Long: `Inspect reference edges of a node, with direction and type filtering.

Examples:
  opcuactl browse refs --endpoint opc.tcp://localhost:4840 --node i=85
  opcuactl browse refs -e opc.tcp://localhost:4840 -n i=85 --direction forward
  opcuactl browse refs --endpoint opc.tcp://localhost:4840 --node i=2253 --include-subtypes --format json`,
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

			nodeID := ua.NewNumericNodeID(0, id.ObjectsFolder)
			if nodeStr != "" {
				nodeID, err = parse.NodeID(nodeStr)
				if err != nil {
					return exitCode(1, err)
				}
			}

			dir := ua.BrowseDirectionBoth
			if direction != "" {
				dir = ua.BrowseDirectionFromString(direction)
			}

			classMask := parse.NodeClassMask(nodeClass)
			refTypeID := uint32(id.References)
			if refType != "" {
				rtID, err := parse.NodeID(refType)
				if err == nil && rtID.IntID() > 0 {
					refTypeID = rtID.IntID()
				}
			}

			n := c.Node(nodeID)
			refs, err := n.References(opCtx, refTypeID, dir, classMask, includeSubtypes)
			if err != nil {
				format.Errf("browsing references of %s: %v", nodeID, err)
				return exitCode(3, err)
			}

			var results []model.Reference
			for _, ref := range refs {
				r := model.Reference{
					IsForward: ref.IsForward,
					NodeClass: ref.NodeClass.String(),
				}
				if ref.ReferenceTypeID != nil {
					r.ReferenceType = ref.ReferenceTypeID.String()
				}
				if ref.NodeID != nil && ref.NodeID.NodeID != nil {
					r.NodeID = ref.NodeID.NodeID.String()
				}
				if ref.BrowseName != nil {
					r.BrowseName = ref.BrowseName.Name
				}
				if ref.DisplayName != nil {
					r.DisplayName = ref.DisplayName.Text
				}
				if ref.TypeDefinition != nil && ref.TypeDefinition.NodeID != nil {
					r.TypeDefinition = ref.TypeDefinition.NodeID.String()
				}
				results = append(results, r)
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"DIRECTION", "REFERENCE TYPE", "NODE ID", "BROWSE NAME", "DISPLAY NAME", "CLASS"}
				rows := make([][]string, len(results))
				for i, r := range results {
					dir := "inverse"
					if r.IsForward {
						dir = "forward"
					}
					rows[i] = []string{dir, r.ReferenceType, r.NodeID, r.BrowseName, r.DisplayName, r.NodeClass}
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(results)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Node ID (default: Objects folder ns=0;i=85)")
	cmd.Flags().StringVar(&direction, "direction", "both", "Browse direction (forward|inverse|both)")
	cmd.Flags().StringVar(&refType, "refs", "", "Reference type ID")
	cmd.Flags().BoolVar(&includeSubtypes, "include-subtypes", true, "Include reference subtypes")
	cmd.Flags().StringVar(&nodeClass, "node-class", "all", "Filter by node class")
	return cmd
}
