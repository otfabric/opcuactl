// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"strings"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/opcuactl/internal/format"
	"github.com/otfabric/opcuactl/internal/model"
	"github.com/spf13/cobra"
)

func newFxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fx",
		Short: "Inspect the OPC UA FX information model of a server",
		Long: `Work with OPC UA FX (UAFX, OPC 10000-81): the information model of
field-level communication. fx inspect reads the UAFX model of a server: its
AutomationComponents with their Assets, FunctionalEntities and
ConnectionEndpoints, the ConnectionManager, and where the address space
deviates from Part 81.

opcuactl reads the model only: it does not establish, verify or close
connections (EstablishConnections and the other Methods of Part 81).

Examples:
  opcuactl fx inspect -e opc.tcp://localhost:4840
  opcuactl fx inspect -e opc.tcp://localhost:4840 --format json
  opcuactl fx inspect -e opc.tcp://localhost:4840 --strict`,
	}
	cmd.AddCommand(newFxInspectCmd())
	return cmd
}

func newFxInspectCmd() *cobra.Command {
	var (
		limits uafx.Limits
		strict bool
	)
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Read the UAFX model of a server and report deviations from Part 81",
		Long: `Read the OPC UA FX model of a server (OPC 10000-81 version 1.00.04):

  - the UAFX namespaces (DI, Data, AC, CM) with their NamespaceMetadata
    versions, and the ServerCapabilities (FXAC, FXCM),
  - FxRoot (§12.2) and the AutomationComponents it organizes (§6.2.2):
    ConformanceName, AggregatedHealth, Descriptors, the Assets with their
    nameplate (§6.3.2), the FunctionalEntities and SubFunctionalEntities
    (§6.4.2) and their ConnectionEndpoints (§6.6.2: Status, IsPersistent,
    CleanupTimeout),
  - the ConnectionManager (§12.3) with its ConnectionConfigurationSets,
  - findings: where the address space deviates from Part 81 (a missing
    namespace or FxRoot, missing mandatory members, values that cannot be
    decoded), each with its clause.

"UAFX server: yes" means the server has the namespaces of §14.2 and FxRoot;
it is not a conformance statement, and the behaviour of the model is not
inspected.

--max-components, --max-children, --max-depth and --max-nodes bound what a
large or hostile server makes opcuactl read (0: the default of go-opcua,
256, 1024, 8 and 10000). A server that holds more is reported up to the
limits, with exit 7.

Exit codes: 0 when the model was read, also with findings or when the
server has no UAFX model; with --strict 1 when the server is not a UAFX
server or there are findings; 7 when a limit cut the inspection short (1
wins with --strict and findings); 4, 5 or 6 when a service fails.

Examples:
  opcuactl fx inspect -e opc.tcp://localhost:4840
  opcuactl fx inspect -e opc.tcp://localhost:4840 --format json
  opcuactl fx inspect -e opc.tcp://localhost:4840 --strict
  opcuactl fx inspect -e opc.tcp://localhost:4840 --max-nodes 500`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			for name, v := range map[string]int{
				"max-components": limits.MaxComponents,
				"max-children":   limits.MaxChildren,
				"max-depth":      limits.MaxDepth,
				"max-nodes":      limits.MaxNodes,
			} {
				if v < 0 {
					return exitCodef(exitUsage, "--%s must not be negative", name)
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(func(ctx context.Context, c *opcua.Client) error {
				r, err := uafx.Inspect(ctx, c, limits)
				if err != nil {
					format.Errf("inspecting the UAFX model: %v", err)
					return exitCode(exitProtocol, err)
				}
				rep := fxReport(r)
				if format.ParseFormat(outputFormat) == format.Table {
					printFxReport(rep)
				} else if err := newWriter().Write(rep); err != nil {
					return err
				}
				return fxResult(rep, strict)
			})
		},
	}
	f := cmd.Flags()
	f.IntVar(&limits.MaxComponents, "max-components", 0, "Most AutomationComponents and ConnectionManagers read (0: 256)")
	f.IntVar(&limits.MaxChildren, "max-children", 0, "Most children read of one node (0: 1024)")
	f.IntVar(&limits.MaxDepth, "max-depth", 0, "Most levels of SubFunctionalEntities read (0: 8)")
	f.IntVar(&limits.MaxNodes, "max-nodes", 0, "Most nodes read in all; more ends with the report so far and exit 7 (0: 10000)")
	f.BoolVar(&strict, "strict", false, "Exit 1 when the server is not a UAFX server or there are findings")
	return cmd
}

// fxResult is the exit of fx inspect for the report rep.
func fxResult(rep model.FxReport, strict bool) error {
	switch {
	case strict && !rep.IsFxServer:
		return exitCodef(exitFindings, "the server is not a UAFX server (Part 81 §12.2, §14.2)")
	case strict && len(rep.Findings) > 0:
		return exitCodef(exitFindings, "%d finding(s)", len(rep.Findings))
	case rep.Truncated:
		return exitCodef(exitPartial, "the server holds more than the limits of the inspection: the report is incomplete")
	}
	if !rep.IsFxServer {
		format.Infof("the server is not a UAFX server: it has no FxRoot or not the namespaces of Part 81 §14.2")
	}
	return nil
}

func fxReport(r *uafx.Report) model.FxReport {
	rep := model.FxReport{
		HasFxNamespaces:      r.HasFXNamespaces(),
		IsFxServer:           r.IsFXServer(),
		Namespaces:           []model.FxNamespace{},
		ServerCapabilities:   r.ServerCapabilities,
		AutomationComponents: []model.FxAutomationComponent{},
		ConnectionManagers:   []model.FxConnectionManager{},
		Findings:             []model.FxFinding{},
		Truncated:            r.Truncated,
	}
	if r.FxRoot != nil {
		rep.FxRoot = r.FxRoot.String()
	}
	for _, n := range r.Namespaces {
		ns := model.FxNamespace{URI: n.URI, Present: n.Present, Index: n.Index, Version: n.Version}
		if !n.PublicationDate.IsZero() {
			ns.PublicationDate = n.PublicationDate.UTC().Format(time.RFC3339)
		}
		rep.Namespaces = append(rep.Namespaces, ns)
	}
	for _, ac := range r.AutomationComponents {
		m := model.FxAutomationComponent{
			FxNode:             fxNode(ac.Node),
			ConformanceName:    ac.ConformanceName,
			Assets:             []model.FxAsset{},
			FunctionalEntities: []model.FxFunctionalEntity{},
		}
		if h := ac.AggregatedHealth; h != nil {
			m.AggregatedHealth = &model.FxHealth{
				DeviceHealth:          h.AggregatedDeviceHealth.String(),
				DeviceHealthBits:      uint16(h.AggregatedDeviceHealth),
				OperationalHealth:     h.AggregatedOperationalHealth.String(),
				OperationalHealthBits: uint32(h.AggregatedOperationalHealth),
			}
		}
		for _, a := range ac.Assets {
			m.Assets = append(m.Assets, model.FxAsset{
				FxNode:             fxNode(a.Node),
				Manufacturer:       a.Manufacturer,
				ManufacturerURI:    a.ManufacturerURI,
				Model:              a.Model,
				ProductCode:        a.ProductCode,
				HardwareRevision:   a.HardwareRevision,
				SoftwareRevision:   a.SoftwareRevision,
				DeviceRevision:     a.DeviceRevision,
				SerialNumber:       a.SerialNumber,
				ProductInstanceURI: a.ProductInstanceURI,
				AssetID:            a.AssetID,
				ComponentName:      a.ComponentName,
				DeviceClass:        a.DeviceClass,
			})
		}
		for _, fe := range ac.FunctionalEntities {
			m.FunctionalEntities = append(m.FunctionalEntities, fxEntity(fe))
		}
		for _, d := range ac.Descriptors {
			m.Descriptors = append(m.Descriptors, fxNode(d))
		}
		rep.AutomationComponents = append(rep.AutomationComponents, m)
	}
	for _, cm := range r.ConnectionManagers {
		m := model.FxConnectionManager{FxNode: fxNode(cm.Node), ConnectionConfigurationSets: []model.FxNode{}}
		for _, s := range cm.ConnectionConfigurationSets {
			m.ConnectionConfigurationSets = append(m.ConnectionConfigurationSets, fxNode(s))
		}
		rep.ConnectionManagers = append(rep.ConnectionManagers, m)
	}
	for _, f := range r.Findings {
		m := model.FxFinding{Path: f.Path, Clause: f.Clause, Text: f.Text}
		if f.Node != nil {
			m.NodeID = f.Node.String()
		}
		rep.Findings = append(rep.Findings, m)
	}
	return rep
}

func fxNode(n uafx.Node) model.FxNode {
	m := model.FxNode{DisplayName: n.DisplayName}
	if n.NodeID != nil {
		m.NodeID = n.NodeID.String()
	}
	if n.BrowseName != nil {
		m.BrowseName = formatBrowseName(n.BrowseName)
	}
	if n.TypeDefinition != nil {
		m.TypeDefinition = n.TypeDefinition.String()
	}
	return m
}

func fxEntity(fe uafx.FunctionalEntity) model.FxFunctionalEntity {
	m := model.FxFunctionalEntity{
		FxNode:                   fxNode(fe.Node),
		AuthorURI:                fe.AuthorURI,
		AuthorAssignedIdentifier: fe.AuthorAssignedIdentifier,
		ConnectionEndpoints:      []model.FxConnectionEndpoint{},
	}
	if v := fe.AuthorAssignedVersion; v != nil {
		m.AuthorAssignedVersion = v.String()
	}
	if h := fe.OperationalHealth; h != nil {
		bits := uint32(*h)
		m.OperationalHealth, m.OperationalHealthBits = h.String(), &bits
	}
	for _, ep := range fe.ConnectionEndpoints {
		e := model.FxConnectionEndpoint{
			FxNode:                          fxNode(ep.Node),
			IsPersistent:                    ep.IsPersistent,
			CleanupTimeoutMs:                ep.CleanupTimeout.Milliseconds(),
			ConnectionManagerApplicationURI: ep.ConnectionManagerApplicationURI,
		}
		if ep.Status != nil {
			e.Status = ep.Status.String()
		}
		m.ConnectionEndpoints = append(m.ConnectionEndpoints, e)
	}
	for _, sub := range fe.SubFunctionalEntities {
		m.SubFunctionalEntities = append(m.SubFunctionalEntities, fxEntity(sub))
	}
	return m
}

// printFxReport writes rep as text, the strings of the server sanitized.
func printFxReport(rep model.FxReport) {
	yes := map[bool]string{true: "yes", false: "no"}
	format.Printf("UAFX server: %s\n", yes[rep.IsFxServer])
	for _, n := range rep.Namespaces {
		if n.Present {
			v := n.Version
			if v == "" {
				v = "-"
			}
			format.Printf("  ns=%-3d %s  %s\n", n.Index, n.URI, v)
		} else {
			format.Printf("  absent %s\n", n.URI)
		}
	}
	if len(rep.ServerCapabilities) > 0 {
		format.Printf("Server capabilities: %s\n", strings.Join(rep.ServerCapabilities, ", "))
	}
	if rep.FxRoot != "" {
		format.Printf("FxRoot: %s\n", rep.FxRoot)
	}
	for _, ac := range rep.AutomationComponents {
		format.Printf("\nAutomationComponent %s (%s)\n", ac.BrowseName, ac.NodeID)
		if ac.ConformanceName != "" {
			format.Printf("  conformance: %s\n", ac.ConformanceName)
		}
		if h := ac.AggregatedHealth; h != nil {
			format.Printf("  health: device %s, operational %s\n", h.DeviceHealth, h.OperationalHealth)
		}
		for _, a := range ac.Assets {
			format.Printf("  asset %s (%s): %s\n", a.BrowseName, a.NodeID, nameplate(a))
		}
		for _, fe := range ac.FunctionalEntities {
			printFxEntity(fe, "  ")
		}
		for _, d := range ac.Descriptors {
			format.Printf("  descriptor %s (%s)\n", d.BrowseName, d.NodeID)
		}
	}
	for _, cm := range rep.ConnectionManagers {
		format.Printf("\nConnectionManager %s (%s): %d connection configuration set(s)\n",
			cm.BrowseName, cm.NodeID, len(cm.ConnectionConfigurationSets))
		for _, s := range cm.ConnectionConfigurationSets {
			format.Printf("  %s (%s)\n", s.BrowseName, s.NodeID)
		}
	}
	if len(rep.Findings) > 0 {
		format.Printf("\nFindings:\n")
		for _, f := range rep.Findings {
			where := f.Path
			if where == "" {
				where = "server"
			}
			format.Printf("  %s: %s (Part 81 §%s)\n", where, f.Text, f.Clause)
		}
	}
	if rep.Truncated {
		format.Printf("\nThe server holds more than the limits of the inspection: the report is incomplete.\n")
	}
}

// nameplate is the nameplate of a as one line.
func nameplate(a model.FxAsset) string {
	var parts []string
	for _, p := range [][2]string{
		{"", a.Manufacturer}, {"", a.Model}, {"serial ", a.SerialNumber},
		{"hardware ", a.HardwareRevision}, {"software ", a.SoftwareRevision},
	} {
		if p[1] != "" {
			parts = append(parts, p[0]+p[1])
		}
	}
	if len(parts) == 0 {
		return "no nameplate"
	}
	return strings.Join(parts, ", ")
}

func printFxEntity(fe model.FxFunctionalEntity, indent string) {
	line := indent + "functional entity " + fe.BrowseName + " (" + fe.NodeID + ")"
	if fe.AuthorAssignedIdentifier != "" {
		line += " " + fe.AuthorAssignedIdentifier
	}
	if fe.AuthorAssignedVersion != "" {
		line += " v" + fe.AuthorAssignedVersion
	}
	if fe.OperationalHealth != "" {
		line += ", health " + fe.OperationalHealth
	}
	format.Printf("%s\n", line)
	for _, ep := range fe.ConnectionEndpoints {
		status := ep.Status
		if status == "" {
			status = "status unknown"
		}
		format.Printf("%s  endpoint %s (%s): %s, persistent %v, cleanup timeout %s\n",
			indent, ep.BrowseName, ep.NodeID, status, ep.IsPersistent,
			(time.Duration(ep.CleanupTimeoutMs) * time.Millisecond).String())
	}
	for _, sub := range fe.SubFunctionalEntities {
		printFxEntity(sub, indent+"  ")
	}
}
