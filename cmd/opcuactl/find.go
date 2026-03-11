package opcuactl

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/otfabric/opcua"
	"github.com/otfabric/opcua/id"
	"github.com/otfabric/opcua/ua"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/otfabric/opcuactl/internal/parse"
	"github.com/spf13/cobra"
)

func newFindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "find",
		Short: "Search nodes and namespaces",
		Long: `Search for nodes by name or pattern and inspect namespaces.

Examples:
  opcuactl find namespace --endpoint opc.tcp://localhost:4840
  opcuactl find node --endpoint opc.tcp://localhost:4840 --name ServerStatus`,
	}
	cmd.AddCommand(newFindNamespaceCmd(), newFindNodeCmd(), newFindTypeCmd())
	return cmd
}

func newFindNamespaceCmd() *cobra.Command {
	var filter string

	cmd := &cobra.Command{
		Use:   "namespace",
		Short: "List or search namespaces",
		Long: `Print the namespace array from the server, optionally filtering by URI substring.

Examples:
  opcuactl find namespace --endpoint opc.tcp://localhost:4840
  opcuactl find namespace --endpoint opc.tcp://localhost:4840 --filter "opcfoundation"
  opcuactl find namespace -e opc.tcp://localhost:4840 --format json`,
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

			nss, err := c.NamespaceArray(opCtx)
			if err != nil {
				format.Errf("reading namespace array: %v", err)
				return exitCode(3, err)
			}

			var entries []model.NamespaceEntry
			for i, uri := range nss {
				if filter != "" && !strings.Contains(strings.ToLower(uri), strings.ToLower(filter)) {
					continue
				}
				entries = append(entries, model.NamespaceEntry{Index: i, URI: uri})
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"INDEX", "NAMESPACE URI"}
				rows := make([][]string, len(entries))
				for i, e := range entries {
					rows[i] = []string{fmt.Sprintf("%d", e.Index), e.URI}
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(entries)
		},
	}

	cmd.Flags().StringVar(&filter, "filter", "", "Filter namespaces by URI substring")
	return cmd
}

func newFindNodeCmd() *cobra.Command {
	var (
		startNode   string
		name        string
		displayName string
		useRegex    bool
		nodeClass   string
		maxDepth    int
		limit       int
	)

	cmd := &cobra.Command{
		Use:   "node",
		Short: "Search nodes by name or pattern",
		Long: `Recursively search for nodes matching a browse name, display name, or pattern.

Examples:
  opcuactl find node --endpoint opc.tcp://localhost:4840 --name ServerStatus
  opcuactl find node -e opc.tcp://localhost:4840 --display-name "Current Time" --format json
  opcuactl find node --endpoint opc.tcp://localhost:4840 --name "Temp.*" --regex --class variable`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" && displayName == "" {
				return exitCodef(1, "at least one of --name or --display-name is required")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			nodeID := ua.NewNumericNodeID(0, id.ObjectsFolder)
			if startNode != "" {
				nodeID, err = parse.NodeID(startNode)
				if err != nil {
					return exitCode(1, err)
				}
			}

			var nameRe, displayRe *regexp.Regexp
			if useRegex {
				if name != "" {
					nameRe, err = regexp.Compile("(?i)" + name)
					if err != nil {
						return exitCodef(1, "invalid --name regex: %v", err)
					}
				}
				if displayName != "" {
					displayRe, err = regexp.Compile("(?i)" + displayName)
					if err != nil {
						return exitCodef(1, "invalid --display-name regex: %v", err)
					}
				}
			}

			classMask := parse.NodeClassMask(nodeClass)
			n := c.Node(nodeID)
			var results []model.NodeInfo

			for wr, walkErr := range n.WalkLimitDedup(ctx, walkLimitDepth(maxDepth)) {
				if walkErr != nil {
					format.Errf("walk: %v", walkErr)
					return exitCode(3, walkErr)
				}
				ref := wr.Ref
				if classMask != ua.NodeClassAll && ref.NodeClass&classMask == 0 {
					continue
				}

				browseName := ""
				if ref.BrowseName != nil {
					browseName = formatBrowseName(ref.BrowseName)
				}
				dispName := ""
				if ref.DisplayName != nil {
					dispName = ref.DisplayName.Text
				}

				if !matchesFilter(browseName, name, nameRe, useRegex) && !matchesFilter(dispName, displayName, displayRe, useRegex) {
					continue
				}

				nid := ""
				if ref.NodeID != nil && ref.NodeID.NodeID != nil {
					nid = ref.NodeID.NodeID.String()
				}

				info := model.NodeInfo{
					NodeID:      nid,
					NodeClass:   ref.NodeClass.String(),
					BrowseName:  browseName,
					DisplayName: dispName,
				}
				if ref.TypeDefinition != nil && ref.TypeDefinition.NodeID != nil {
					info.TypeDefinition = opcua.TypeDefinitionDisplayName(ref.TypeDefinition.NodeID)
				}
				// Optionally enrich with summary
				results = append(results, enrichNodeInfo(ctx, c, info))

				if limit > 0 && len(results) >= limit {
					break
				}
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

	cmd.Flags().StringVar(&startNode, "start-node", "", "Start node ID (default: Objects folder)")
	cmd.Flags().StringVar(&name, "name", "", "Match browse name (exact, contains, or regex with --regex)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "Match display name (exact, contains, or regex with --regex)")
	cmd.Flags().BoolVar(&useRegex, "regex", false, "Treat --name and --display-name as regex patterns")
	cmd.Flags().StringVar(&nodeClass, "class", "all", "Filter by node class")
	cmd.Flags().IntVar(&maxDepth, "depth", 0, "Maximum search depth (0 = unlimited)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of results (0 = unlimited)")
	_ = cmd.RegisterFlagCompletionFunc("class", fixedCompletion(nodeClassValues))
	return cmd
}

// walkLimitDepth converts the CLI --depth convention (0 = unlimited) to the
// SDK WalkLimit convention (-1 = unlimited).
func walkLimitDepth(d int) int {
	if d == 0 {
		return -1
	}
	return d
}

func matchesFilter(value, pattern string, re *regexp.Regexp, useRegex bool) bool {
	if pattern == "" && re == nil {
		return false
	}
	if useRegex && re != nil {
		return re.MatchString(value)
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(pattern))
}

func enrichNodeInfo(ctx context.Context, c *opcua.Client, info model.NodeInfo) model.NodeInfo {
	if info.NodeID == "" {
		return info
	}
	nid, err := parse.NodeID(info.NodeID)
	if err != nil {
		return info
	}
	opCtx, cancel := connCfg.TimeoutContext(ctx)
	defer cancel()

	summary, err := c.Node(nid).Summary(opCtx)
	if err != nil {
		return info
	}
	if summary.DataType != nil {
		info.DataType = opcua.DataTypeDisplayName(summary.DataType)
	}
	if summary.Description != nil && summary.Description.Text != "" {
		info.Description = summary.Description.Text
	}
	return info
}

func newFindTypeCmd() *cobra.Command {
	var (
		startNode   string
		typeDefStr  string
		dataTypeStr string
		nodeClass   string
		maxDepth    int
		limit       int
	)

	cmd := &cobra.Command{
		Use:   "type",
		Short: "Find nodes by type definition or data type",
		Long: `Recursively search for nodes matching a type definition or data type.

Match by NodeID (e.g. i=2041) or by friendly/display name substring.

Examples:
  opcuactl find type -e opc.tcp://localhost:4840 --type-definition i=58
  opcuactl find type -e opc.tcp://localhost:4840 --data-type Double
  opcuactl find type -e opc.tcp://localhost:4840 --data-type i=11 --class variable --format json
  opcuactl find type -e opc.tcp://localhost:4840 --type-definition BaseObjectType --limit 20`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if typeDefStr == "" && dataTypeStr == "" {
				return exitCodef(1, "at least one of --type-definition or --data-type is required")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			c, err := connCfg.NewClient(ctx)
			if err != nil {
				format.Errf("%v", err)
				return exitCode(2, err)
			}
			defer func() { _ = c.Close(ctx) }()

			nodeID := ua.NewNumericNodeID(0, id.ObjectsFolder)
			if startNode != "" {
				nodeID, err = parse.NodeID(startNode)
				if err != nil {
					return exitCode(1, err)
				}
			}

			// Try to parse as NodeID, otherwise use as name match
			var typeDefID *ua.NodeID
			typeDefName := ""
			if typeDefStr != "" {
				if nid, err := parse.NodeID(typeDefStr); err == nil {
					typeDefID = nid
				} else {
					typeDefName = strings.ToLower(typeDefStr)
				}
			}
			var dataTypeID *ua.NodeID
			dataTypeName := ""
			if dataTypeStr != "" {
				if nid, err := parse.NodeID(dataTypeStr); err == nil {
					dataTypeID = nid
				} else {
					dataTypeName = strings.ToLower(dataTypeStr)
				}
			}

			classMask := parse.NodeClassMask(nodeClass)
			n := c.Node(nodeID)
			var results []model.NodeInfo

			for wr, walkErr := range n.WalkLimitDedup(ctx, walkLimitDepth(maxDepth)) {
				if walkErr != nil {
					format.Errf("walk: %v", walkErr)
					return exitCode(3, walkErr)
				}
				ref := wr.Ref
				if classMask != ua.NodeClassAll && ref.NodeClass&classMask == 0 {
					continue
				}

				nid := ""
				if ref.NodeID != nil && ref.NodeID.NodeID != nil {
					nid = ref.NodeID.NodeID.String()
				}
				if nid == "" {
					continue
				}

				// Get summary for type matching
				opCtx, opCancel := connCfg.TimeoutContext(ctx)
				summary, summErr := c.Node(ref.NodeID.NodeID).Summary(opCtx)
				opCancel()
				if summErr != nil {
					continue
				}

				// Match type definition
				if typeDefID != nil || typeDefName != "" {
					if summary.TypeDefinition == nil {
						continue
					}
					if typeDefID != nil && !summary.TypeDefinition.Equal(typeDefID) {
						continue
					}
					if typeDefName != "" {
						tdFriendly := strings.ToLower(opcua.TypeDefinitionDisplayName(summary.TypeDefinition))
						if !strings.Contains(tdFriendly, typeDefName) {
							// Also try browsing for display name of the type
							continue
						}
					}
				}

				// Match data type
				if dataTypeID != nil || dataTypeName != "" {
					if summary.DataType == nil {
						continue
					}
					if dataTypeID != nil && !summary.DataType.Equal(dataTypeID) {
						continue
					}
					if dataTypeName != "" {
						dtStr := strings.ToLower(summary.DataType.String())
						dtFriendly := strings.ToLower(opcua.DataTypeDisplayName(summary.DataType))
						if !strings.Contains(dtStr, dataTypeName) && !strings.Contains(dtFriendly, dataTypeName) {
							continue
						}
					}
				}

				info := model.NodeInfo{
					NodeID:    nid,
					NodeClass: summary.NodeClass.String(),
				}
				if summary.BrowseName != nil {
					info.BrowseName = formatBrowseName(summary.BrowseName)
				}
				if summary.DisplayName != nil {
					info.DisplayName = summary.DisplayName.Text
				}
				if summary.Description != nil {
					info.Description = summary.Description.Text
				}
				if summary.DataType != nil {
					info.DataType = opcua.DataTypeDisplayName(summary.DataType)
				}
				if summary.TypeDefinition != nil {
					info.TypeDefinition = opcua.TypeDefinitionDisplayName(summary.TypeDefinition)
				}
				results = append(results, info)

				if limit > 0 && len(results) >= limit {
					break
				}
			}

			if verbose {
				format.Errf("found %d node(s)", len(results))
			}

			w := newWriter()
			f := format.ParseFormat(outputFormat)
			if f == format.Table {
				headers := []string{"NODE ID", "CLASS", "DISPLAY NAME", "DATA TYPE", "TYPE DEFINITION"}
				rows := make([][]string, len(results))
				for i, r := range results {
					rows[i] = []string{r.NodeID, r.NodeClass, r.DisplayName, r.DataType, r.TypeDefinition}
				}
				return w.WriteTable(headers, rows)
			}
			return w.Write(results)
		},
	}

	cmd.Flags().StringVar(&startNode, "start-node", "", "Start node ID (default: Objects folder)")
	cmd.Flags().StringVar(&typeDefStr, "type-definition", "", "Match type definition by NodeID or name")
	cmd.Flags().StringVar(&dataTypeStr, "data-type", "", "Match data type by NodeID or name")
	cmd.Flags().StringVar(&nodeClass, "class", "all", "Filter by node class")
	cmd.Flags().IntVar(&maxDepth, "depth", 0, "Maximum search depth (0 = unlimited)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of results (0 = unlimited)")
	_ = cmd.RegisterFlagCompletionFunc("class", fixedCompletion(nodeClassValues))
	return cmd
}
