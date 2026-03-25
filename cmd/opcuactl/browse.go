package opcuactl

import (
	"context"
	"fmt"
	"strings"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
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
  opcuactl browse tree --endpoint opc.tcp://localhost:4840 --depth 3
  opcuactl browse refs --endpoint opc.tcp://localhost:4840 --node i=85`,
	}
	cmd.AddCommand(newBrowseChildrenCmd(), newBrowseRefsCmd(), newBrowseTreeCmd(), newBrowsePathCmd())
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
					cn.BrowseName = formatBrowseName(summary.BrowseName)
				}
				if summary.DisplayName != nil {
					cn.DisplayName = summary.DisplayName.Text
				}
				if summary.TypeDefinition != nil {
					cn.TypeDefinition = ua.TypeDefinitionDisplayName(summary.TypeDefinition)
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
	_ = cmd.RegisterFlagCompletionFunc("node-class", fixedCompletion(nodeClassValues))
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
					r.ReferenceType = ua.ReferenceTypeDisplayName(ref.ReferenceTypeID)
				}
				if ref.NodeID != nil && ref.NodeID.NodeID != nil {
					r.NodeID = ref.NodeID.NodeID.String()
				}
				if ref.BrowseName != nil {
					r.BrowseName = formatBrowseName(ref.BrowseName)
				}
				if ref.DisplayName != nil {
					r.DisplayName = ref.DisplayName.Text
				}
				if ref.TypeDefinition != nil && ref.TypeDefinition.NodeID != nil {
					r.TypeDefinition = ua.TypeDefinitionDisplayName(ref.TypeDefinition.NodeID)
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
	_ = cmd.RegisterFlagCompletionFunc("direction", fixedCompletion(directionValues))
	_ = cmd.RegisterFlagCompletionFunc("node-class", fixedCompletion(nodeClassValues))
	return cmd
}

func newBrowseTreeCmd() *cobra.Command {
	var (
		nodeStr   string
		maxDepth  int
		nodeClass string
	)

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Recursively browse the address space",
		Long: `Recursively print an address-space tree from a start node.

Examples:
  opcuactl browse tree --endpoint opc.tcp://localhost:4840
  opcuactl browse tree --endpoint opc.tcp://localhost:4840 --depth 3
  opcuactl browse tree -e opc.tcp://localhost:4840 -n i=2253 --node-class variable --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			nodeID := ua.NewNumericNodeID(0, id.ObjectsFolder)
			if nodeStr != "" {
				nodeID, err = parse.NodeID(nodeStr)
				if err != nil {
					return exitCode(1, err)
				}
			}

			classMask := parse.NodeClassMask(nodeClass)
			n := c.Node(nodeID)

			w := newWriter()
			f := format.ParseFormat(outputFormat)

			if f == format.Table {
				return browseTreeTable(ctx, n, maxDepth, classMask)
			}
			// JSON/YAML: collect tree structure
			tree, err := browseTreeJSON(ctx, n, maxDepth, classMask, 0)
			if err != nil {
				format.Errf("browsing tree: %v", err)
				return exitCode(3, err)
			}
			return w.Write(tree)
		},
	}

	cmd.Flags().StringVarP(&nodeStr, "node", "n", "", "Start node ID (default: Objects folder ns=0;i=85)")
	cmd.Flags().IntVar(&maxDepth, "depth", 5, "Maximum tree depth")
	cmd.Flags().StringVar(&nodeClass, "node-class", "all", "Filter by node class")
	_ = cmd.RegisterFlagCompletionFunc("node-class", fixedCompletion(nodeClassValues))
	return cmd
}

func browseTreeTable(ctx context.Context, n *opcua.Node, maxDepth int, classMask ua.NodeClass) error {
	for wr, err := range n.WalkLimitDedup(ctx, maxDepth) {
		if err != nil {
			format.Errf("walk: %v", err)
			return exitCode(3, err)
		}
		ref := wr.Ref
		if classMask != ua.NodeClassAll && ref.NodeClass&classMask == 0 {
			continue
		}
		nid := ""
		if ref.NodeID != nil && ref.NodeID.NodeID != nil {
			nid = ref.NodeID.NodeID.String()
		}
		indent := strings.Repeat("  ", wr.Depth)
		name := ""
		if ref.DisplayName != nil {
			name = ref.DisplayName.Text
		}
		fmt.Printf("%s%s (%s) [%s]\n", indent, name, nid, ref.NodeClass)
	}
	return nil
}

func browseTreeJSON(ctx context.Context, n *opcua.Node, maxDepth int, classMask ua.NodeClass, depth int) (model.TreeNode, error) {
	opCtx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()

	tn := model.TreeNode{
		NodeID: n.ID.String(),
		Depth:  depth,
	}

	summary, err := n.Summary(opCtx)
	if err == nil {
		tn.NodeClass = summary.NodeClass.String()
		if summary.BrowseName != nil {
			tn.BrowseName = formatBrowseName(summary.BrowseName)
		}
		if summary.DisplayName != nil {
			tn.DisplayName = summary.DisplayName.Text
		}
		if summary.TypeDefinition != nil {
			tn.TypeDefinition = ua.TypeDefinitionDisplayName(summary.TypeDefinition)
		}
	}

	if maxDepth > 0 && depth >= maxDepth {
		return tn, nil
	}

	children, err := n.Children(opCtx, id.HierarchicalReferences, classMask)
	if err != nil {
		return tn, nil // leaf or error, return what we have
	}

	for _, child := range children {
		childNode, err := browseTreeJSON(ctx, child, maxDepth, classMask, depth+1)
		if err != nil {
			continue
		}
		tn.Children = append(tn.Children, childNode)
	}
	return tn, nil
}

func newBrowsePathCmd() *cobra.Command {
	var (
		pathStr   string
		ns        uint16
		startNode string
	)

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Resolve a browse path to a node ID",
		Long: `Resolve a dot-separated browse path to a NodeID.

By default, paths are resolved from the Objects folder in namespace 0.
Use --namespace to resolve paths in a different namespace.
Use --start-node to resolve from a custom starting node.

Examples:
  opcuactl browse path -e opc.tcp://localhost:4840 --path Server.ServerStatus
  opcuactl browse path -e opc.tcp://localhost:4840 --path MyFolder.Sensor.Temperature --namespace 1
  opcuactl browse path -e opc.tcp://localhost:4840 --path Sensor.Value --start-node "ns=1;i=1000"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if pathStr == "" {
				return exitCodef(1, "--path is required")
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

			var result model.BrowsePathResult
			result.Path = pathStr

			if startNode != "" {
				// Custom start node: use Node.TranslateBrowsePathInNamespaceToNodeID
				startID, err := parse.NodeID(startNode)
				if err != nil {
					return exitCode(1, err)
				}

				n := c.Node(startID)
				resolved, err := n.TranslateBrowsePathInNamespaceToNodeID(opCtx, ns, pathStr)
				if err != nil {
					format.Errf("resolving path %q from %s: %v", pathStr, startID, err)
					return exitCode(3, err)
				}
				result.NodeID = resolved.String()
			} else if isQualifiedPath(pathStr) {
				// Namespace-qualified syntax: "0:Server.0:ServerStatus" or "2:DeviceSet.4:PLC"
				node, err := c.NodeFromQualifiedPath(opCtx, pathStr)
				if err != nil {
					format.Errf("resolving qualified path %q: %v", pathStr, err)
					return exitCode(3, err)
				}
				result.NodeID = node.ID.String()
			} else if ns > 0 {
				// Namespace-specific: use NodeFromPathInNamespace
				node, err := c.NodeFromPathInNamespace(opCtx, ns, pathStr)
				if err != nil {
					format.Errf("resolving path %q in namespace %d: %v", pathStr, ns, err)
					return exitCode(3, err)
				}
				result.NodeID = node.ID.String()
			} else {
				// Default: namespace 0, from Objects
				node, err := c.NodeFromPath(opCtx, pathStr)
				if err != nil {
					format.Errf("resolving path %q: %v", pathStr, err)
					return exitCode(3, err)
				}
				result.NodeID = node.ID.String()
			}

			if verbose {
				format.Errf("resolved %q → %s", pathStr, result.NodeID)
			}

			if quiet {
				fmt.Println(result.NodeID)
				return nil
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"PATH", "NODE ID"}
				rows := [][]string{{result.Path, result.NodeID}}
				return w.WriteTable(headers, rows)
			}
			return w.Write(result)
		},
	}

	cmd.Flags().StringVar(&pathStr, "path", "", "Dot-separated browse path (required)")
	cmd.Flags().Uint16Var(&ns, "namespace", 0, "Namespace index for path segments")
	cmd.Flags().StringVar(&startNode, "start-node", "", "Custom start node (default: Objects folder)")
	return cmd
}

// isQualifiedPath returns true if the path contains at least one namespace-qualified
// segment in the form "digit:name" (e.g. "0:Server.0:ServerStatus").
func isQualifiedPath(path string) bool {
	for _, seg := range strings.Split(path, ".") {
		if idx := strings.Index(seg, ":"); idx > 0 {
			// Check that the part before ":" is numeric
			allDigits := true
			for _, ch := range seg[:idx] {
				if ch < '0' || ch > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				return true
			}
		}
	}
	return false
}
