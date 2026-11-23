// SPDX-License-Identifier: MIT

package opcuactl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/otfabric/go-opcua/ua"
	"github.com/otfabric/go-opcua/uafx"
	"github.com/otfabric/go-opcua/uafx/fxacid"
	"github.com/otfabric/opcuactl/internal/parse"
	"gopkg.in/yaml.v3"
)

// The request file of fx connect: the input arguments of EstablishConnections
// (Part 81 §6.2.4.2) in a form for people. It is JSON or YAML; YAML is
// converted to JSON first, so both have the same fields. NodeIds take the
// string form of Part 6 §5.3.1.10, also with a namespace URI ("nsu=...;i=5"),
// which is resolved with the NamespaceArray of the server.

// fxRequestFile is the request file.
type fxRequestFile struct {
	// Commands are the names of the commands of the CommandMask (Part 81
	// §10.29): VerifyAsset, VerifyFunctionalEntity, CreateConnectionEndpoint,
	// EstablishControl, SetConfigurationData, ReassignControl,
	// ReserveCommunicationIds, SetCommunicationConfiguration,
	// EnableCommunication.
	Commands                    []string                  `json:"commands"`
	AssetVerifications          []fxAssetVerificationIn   `json:"assetVerifications"`
	Endpoints                   []fxEndpointIn            `json:"endpoints"`
	ReserveCommunicationIDs     []fxReserveIDsIn          `json:"reserveCommunicationIds"`
	CommunicationConfigurations []fxCommunicationConfigIn `json:"communicationConfigurations"`
}

// fxAssetVerificationIn is an AssetVerificationDataType (Part 81 §10.5).
type fxAssetVerificationIn struct {
	Asset string `json:"asset"`
	// Mode: Compatibility, Identity or IdentityAndCompatibility.
	Mode string `json:"mode"`
	// ExpectedResult: Match or Compatible (NotSet and Mismatch are
	// accepted and left to the server).
	ExpectedResult string `json:"expectedResult"`
	// Variables are the ExpectedVerificationVariables: the key is a
	// QualifiedName ("Name", "2:Name" or "nsu=<uri>;Name").
	Variables []fxKeyValueIn `json:"variables"`
	// AdditionalVariables are the ExpectedAdditionalVerificationVariables.
	AdditionalVariables []fxValueIn `json:"additionalVariables"`
}

// fxKeyValueIn is a KeyValuePair with a typed value.
type fxKeyValueIn struct {
	Key   string          `json:"key"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// fxValueIn is a NodeIdValuePair (Part 81 §10.32) with its NodeIdArray
// (§10.31).
type fxValueIn struct {
	Node  string   `json:"node"`
	Index []uint32 `json:"index"`
	// Type is the built-in type of Value (Int32, Double, String, ...) or
	// "ua-json" for a Variant in the OPC UA JSON encoding. Without Type a
	// value must be null: the pair then checks only that the node exists.
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// fxEndpointIn is a ConnectionEndpointConfigurationDataType (Part 81
// §10.14) with its PubSubConnectionEndpointParameterDataType (§10.18.3)
// written out, or the NodeId of an existing ConnectionEndpoint (Node).
type fxEndpointIn struct {
	FunctionalEntity string `json:"functionalEntity"`
	// Node is an existing ConnectionEndpoint; the parameter fields below
	// must then be empty.
	Node string `json:"node"`

	Name string `json:"name"`
	// Type is the ConnectionEndpointTypeId, PubSubConnectionEndpointType of
	// the AC namespace when empty.
	Type    string   `json:"type"`
	Inputs  []string `json:"inputs"`
	Outputs []string `json:"outputs"`
	// Persistent is IsPersistent.
	Persistent bool `json:"persistent"`
	// CleanupTimeoutMs is the CleanupTimeout in milliseconds; negative
	// disables the cleanup, absent is -1.
	CleanupTimeoutMs  *float64     `json:"cleanupTimeoutMs"`
	Related           *fxRelatedIn `json:"relatedEndpoint"`
	Preconfigured     bool         `json:"preconfigured"`
	Mode              string       `json:"mode"`
	Verify            []fxValueIn  `json:"expectedVerificationVariables"`
	ControlGroups     []string     `json:"controlGroups"`
	ConfigurationData []fxValueIn  `json:"configurationData"`
	Links             *fxLinksIn   `json:"communicationLinks"`
}

// fxRelatedIn is a RelatedEndpointDataType (Part 81 §10.42).
type fxRelatedIn struct {
	Address string `json:"address"`
	// Path is the path from FxRoot to the FunctionalEntity of the other
	// end: PortableQualifiedNames as "<namespace URI>;Name" or "Name".
	Path []string `json:"path"`
	Name string   `json:"name"`
}

// fxLinksIn is a PubSubCommunicationLinkConfigurationDataType (Part 81
// §10.13.3).
type fxLinksIn struct {
	Reader                    *fxRefIn     `json:"dataSetReader"`
	ExpectedSubscribedVersion *fxVersionIn `json:"expectedSubscribedDataSetVersion"`
	Writer                    *fxRefIn     `json:"dataSetWriter"`
	ExpectedPublishedVersion  *fxVersionIn `json:"expectedPublishedDataSetVersion"`
}

// fxRefIn is a PubSubConfigurationRefDataType (Part 14 §6.2.12.3).
type fxRefIn struct {
	// Mask are the names of the bits of the ConfigurationMask without
	// their prefix: ElementAdd, ElementMatch, ElementModify, ElementRemove,
	// ReferenceWriter, ReferenceReader, ReferenceWriterGroup,
	// ReferenceReaderGroup, ReferenceConnection, ReferencePubDataset,
	// ReferenceSubDataset, ReferenceSecurityGroup, ReferencePushTarget.
	Mask       []string `json:"mask"`
	Connection uint16   `json:"connection"`
	Group      uint16   `json:"group"`
	Element    uint16   `json:"element"`
}

// fxVersionIn is a ConfigurationVersionDataType.
type fxVersionIn struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
}

// fxReserveIDsIn is a PubSubReserveCommunicationIds2DataType (Part 81
// §10.44.2).
type fxReserveIDsIn struct {
	TransportProfileURI   string `json:"transportProfileUri"`
	WriterGroups          uint16 `json:"writerGroups"`
	DataSetWriters        uint16 `json:"dataSetWriters"`
	TransportSpecificInfo bool   `json:"transportSpecificInfo"`
}

// fxCommunicationConfigIn is a PubSubCommunicationConfigurationDataType
// (Part 81 §10.10.3).
type fxCommunicationConfigIn struct {
	// PubSubConfiguration is a PubSubConfiguration2DataType in the OPC UA
	// JSON encoding, as pubsub config export writes it.
	PubSubConfiguration json.RawMessage `json:"pubsubConfiguration"`
	// PubSubConfigurationFile is a file with PubSubConfiguration, relative
	// to the request file.
	PubSubConfigurationFile string    `json:"pubsubConfigurationFile"`
	RequireCompleteUpdate   bool      `json:"requireCompleteUpdate"`
	References              []fxRefIn `json:"references"`
}

// errFxRequest marks a request file whose content is wrong: a usage error.
var errFxRequest = errors.New("request file")

func fxRequestErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errFxRequest}, args...)...)
}

// readFxRequest reads and decodes the request file path ("-": stdin is
// not supported, the PubSub configuration may name files relative to it).
// It returns the file and the directory its relative paths start from.
func readFxRequest(path string) (*fxRequestFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, exitCodef(exitInvalidInput, "reading --request: %v", err)
	}
	f, err := decodeFxRequest(data)
	if err != nil {
		return nil, exitCode(exitUsage, err)
	}
	for i := range f.CommunicationConfigurations {
		c := &f.CommunicationConfigurations[i]
		if c.PubSubConfigurationFile == "" {
			continue
		}
		if len(c.PubSubConfiguration) > 0 {
			return nil, exitCode(exitUsage, fxRequestErrorf("communicationConfigurations[%d]: pubsubConfiguration and pubsubConfigurationFile are exclusive", i))
		}
		p := c.PubSubConfigurationFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(path), p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, exitCodef(exitInvalidInput, "reading communicationConfigurations[%d].pubsubConfigurationFile: %v", i, err)
		}
		c.PubSubConfiguration = b
	}
	return f, nil
}

// decodeFxRequest decodes a request file in JSON or YAML. Unknown fields are
// an error, so that a misspelt field is not silently dropped.
func decodeFxRequest(data []byte) (*fxRequestFile, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		var y any
		if err := yaml.Unmarshal(data, &y); err != nil {
			return nil, fxRequestErrorf("%v", err)
		}
		if _, ok := y.(map[string]any); !ok {
			return nil, fxRequestErrorf("want an object with commands, endpoints, ...")
		}
		b, err := json.Marshal(y)
		if err != nil {
			return nil, fxRequestErrorf("%v", err)
		}
		data = b
	}
	var f fxRequestFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fxRequestErrorf("%v", err)
	}
	if dec.More() {
		return nil, fxRequestErrorf("trailing data after the request")
	}
	return &f, nil
}

// fxCommandNames are the names of the request file for the commands.
var fxCommandNames = map[string]uafx.FxCommandMask{
	"verifyasset":                   uafx.VerifyAssetCmd,
	"verifyfunctionalentity":        uafx.VerifyFunctionalEntityCmd,
	"createconnectionendpoint":      uafx.CreateConnectionEndpointCmd,
	"establishcontrol":              uafx.EstablishControlCmd,
	"setconfigurationdata":          uafx.SetConfigurationDataCmd,
	"reassigncontrol":               uafx.ReassignControlCmd,
	"reservecommunicationids":       uafx.ReserveCommunicationIDsCmd,
	"setcommunicationconfiguration": uafx.SetCommunicationConfigurationCmd,
	"enablecommunication":           uafx.EnableCommunicationCmd,
}

func fxCommandMask(names []string) (uafx.FxCommandMask, error) {
	if len(names) == 0 {
		return 0, fxRequestErrorf("no commands")
	}
	var m uafx.FxCommandMask
	for _, n := range names {
		key := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "Cmd"))
		c, ok := fxCommandNames[key]
		if !ok {
			return 0, fxRequestErrorf("unknown command %q", n)
		}
		m |= c
	}
	return m, nil
}

var fxRefMaskNames = map[string]ua.PubSubConfigurationRefMask{
	"elementadd":             ua.PubSubConfigurationRefMaskElementAdd,
	"elementmatch":           ua.PubSubConfigurationRefMaskElementMatch,
	"elementmodify":          ua.PubSubConfigurationRefMaskElementModify,
	"elementremove":          ua.PubSubConfigurationRefMaskElementRemove,
	"referencewriter":        ua.PubSubConfigurationRefMaskReferenceWriter,
	"referencereader":        ua.PubSubConfigurationRefMaskReferenceReader,
	"referencewritergroup":   ua.PubSubConfigurationRefMaskReferenceWriterGroup,
	"referencereadergroup":   ua.PubSubConfigurationRefMaskReferenceReaderGroup,
	"referenceconnection":    ua.PubSubConfigurationRefMaskReferenceConnection,
	"referencepubdataset":    ua.PubSubConfigurationRefMaskReferencePubDataset,
	"referencesubdataset":    ua.PubSubConfigurationRefMaskReferenceSubDataset,
	"referencesecuritygroup": ua.PubSubConfigurationRefMaskReferenceSecurityGroup,
	"referencepushtarget":    ua.PubSubConfigurationRefMaskReferencePushTarget,
}

// fxRequestBuilder turns the request file into the request of uafx, with the
// NamespaceArray of the server for "nsu=" NodeIds.
type fxRequestBuilder struct {
	namespaces []string
	ac         uint16 // index of the AC namespace
}

func (b *fxRequestBuilder) nodeID(field, s string) (*ua.NodeID, error) {
	if s == "" {
		return nil, fxRequestErrorf("%s: no NodeId", field)
	}
	if strings.HasPrefix(s, "nsu=") {
		e, err := ua.ParseExpandedNodeID(s, b.namespaces)
		if err != nil {
			return nil, fxRequestErrorf("%s: %v", field, err)
		}
		return e.NodeID, nil
	}
	n, err := parse.NodeID(s)
	if err != nil {
		return nil, fxRequestErrorf("%s: %v", field, err)
	}
	return n, nil
}

func (b *fxRequestBuilder) nodeIDs(field string, ss []string) ([]*ua.NodeID, error) {
	out := make([]*ua.NodeID, 0, len(ss))
	for i, s := range ss {
		n, err := b.nodeID(fmt.Sprintf("%s[%d]", field, i), s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// qualifiedName parses "Name", "2:Name" or "nsu=<uri>;Name".
func (b *fxRequestBuilder) qualifiedName(field, s string) (*ua.QualifiedName, error) {
	if rest, ok := strings.CutPrefix(s, "nsu="); ok {
		uri, name, ok := strings.Cut(rest, ";")
		if !ok || name == "" {
			return nil, fxRequestErrorf("%s: %q is not nsu=<uri>;<name>", field, s)
		}
		for i, u := range b.namespaces {
			if u == uri {
				return &ua.QualifiedName{NamespaceIndex: uint16(i), Name: name}, nil
			}
		}
		return nil, fxRequestErrorf("%s: the server has no namespace %s", field, uri)
	}
	if s == "" {
		return nil, fxRequestErrorf("%s: no name", field)
	}
	return parse.QualifiedName(s), nil
}

func fxValue(field, typ string, raw json.RawMessage) (*ua.Variant, error) {
	null := len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null"
	switch {
	case typ == "" && null:
		return nil, nil
	case typ == "":
		return nil, fxRequestErrorf("%s: a value needs a type", field)
	case null:
		return nil, fxRequestErrorf("%s: type %s without a value", field, typ)
	case parse.IsUAEncodedType(typ):
		if !strings.EqualFold(typ, "ua-json") {
			return nil, fxRequestErrorf("%s: type %s: only ua-json is supported in a request file", field, typ)
		}
		v := new(ua.Variant)
		if err := ua.DecodeJSON(raw, v); err != nil {
			return nil, fxRequestErrorf("%s: %v", field, err)
		}
		return v, nil
	}
	t, err := parse.BuiltinType(typ)
	if err != nil {
		return nil, fxRequestErrorf("%s: %v", field, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var j any
	if err := dec.Decode(&j); err != nil {
		return nil, fxRequestErrorf("%s: %v", field, err)
	}
	v, err := parse.ConvertJSONArgument(j, &ua.Argument{DataType: ua.NewNumericNodeID(0, uint32(t)), ValueRank: -2})
	if err != nil {
		return nil, fxRequestErrorf("%s: %v", field, err)
	}
	vv, err := ua.NewVariant(v)
	if err != nil {
		return nil, fxRequestErrorf("%s: %v", field, err)
	}
	return vv, nil
}

func (b *fxRequestBuilder) pairs(field string, in []fxValueIn) ([]*uafx.NodeIDValuePair, error) {
	out := make([]*uafx.NodeIDValuePair, 0, len(in))
	for i, p := range in {
		f := fmt.Sprintf("%s[%d]", field, i)
		n, err := b.nodeID(f+".node", p.Node)
		if err != nil {
			return nil, err
		}
		v, err := fxValue(f+".value", p.Type, p.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, &uafx.NodeIDValuePair{Key: &uafx.NodeIDArray{Node: n, ArrayIndex: p.Index}, Value: v})
	}
	return out, nil
}

func fxRef(field string, r *fxRefIn) (*ua.PubSubConfigurationRefDataType, error) {
	if r == nil {
		return nil, nil
	}
	var m ua.PubSubConfigurationRefMask
	for _, n := range r.Mask {
		bit, ok := fxRefMaskNames[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(n), "PubSubConfigurationRefMask"))]
		if !ok {
			return nil, fxRequestErrorf("%s: unknown mask bit %q", field, n)
		}
		m |= bit
	}
	return &ua.PubSubConfigurationRefDataType{ConfigurationMask: m, ConnectionIndex: r.Connection, GroupIndex: r.Group, ElementIndex: r.Element}, nil
}

func fxVersion(v *fxVersionIn) *ua.ConfigurationVersionDataType {
	if v == nil {
		return nil
	}
	return &ua.ConfigurationVersionDataType{MajorVersion: v.Major, MinorVersion: v.Minor}
}

var fxModes = map[string]uafx.PubSubConnectionEndpointMode{
	"publishersubscriber": uafx.PubSubPublisherSubscriber,
	"publisher":           uafx.PubSubPublisher,
	"subscriber":          uafx.PubSubSubscriber,
}

var fxAssetModes = map[string]uafx.AssetVerificationMode{
	"compatibility":            uafx.AssetCompatibility,
	"identity":                 uafx.AssetIdentity,
	"identityandcompatibility": uafx.AssetIdentityAndCompatibility,
}

var fxAssetResults = map[string]uafx.AssetVerificationResult{
	"notset":     uafx.AssetVerificationNotSet,
	"match":      uafx.AssetVerificationMatch,
	"compatible": uafx.AssetVerificationCompatible,
	"mismatch":   uafx.AssetVerificationMismatch,
}

func lookup[T any](field, kind, s string, m map[string]T) (T, error) {
	v, ok := m[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		var zero T
		return zero, fxRequestErrorf("%s: unknown %s %q", field, kind, s)
	}
	return v, nil
}

func (b *fxRequestBuilder) endpoint(i int, e fxEndpointIn) (*uafx.ConnectionEndpointConfiguration, error) {
	field := fmt.Sprintf("endpoints[%d]", i)
	fe, err := b.nodeID(field+".functionalEntity", e.FunctionalEntity)
	if err != nil {
		return nil, err
	}
	cfg := &uafx.ConnectionEndpointConfiguration{FunctionalEntityNode: fe}
	if e.Node != "" {
		if e.Name != "" || e.Type != "" || e.Inputs != nil || e.Outputs != nil || e.Persistent || e.CleanupTimeoutMs != nil ||
			e.Related != nil || e.Preconfigured || e.Mode != "" {
			return nil, fxRequestErrorf("%s: node and the parameters of a new endpoint are exclusive", field)
		}
		n, err := b.nodeID(field+".node", e.Node)
		if err != nil {
			return nil, err
		}
		cfg.ConnectionEndpoint = &uafx.ConnectionEndpointDefinition{Node: n}
	} else {
		p, err := b.parameter(field, e)
		if err != nil {
			return nil, err
		}
		cfg.ConnectionEndpoint = &uafx.ConnectionEndpointDefinition{Parameter: p}
	}
	if cfg.ExpectedVerificationVariables, err = b.pairs(field+".expectedVerificationVariables", e.Verify); err != nil {
		return nil, err
	}
	if cfg.ControlGroups, err = b.nodeIDs(field+".controlGroups", e.ControlGroups); err != nil {
		return nil, err
	}
	if cfg.ConfigurationData, err = b.pairs(field+".configurationData", e.ConfigurationData); err != nil {
		return nil, err
	}
	if l := e.Links; l != nil {
		r, err := fxRef(field+".communicationLinks.dataSetReader", l.Reader)
		if err != nil {
			return nil, err
		}
		w, err := fxRef(field+".communicationLinks.dataSetWriter", l.Writer)
		if err != nil {
			return nil, err
		}
		cfg.CommunicationLinks = &uafx.PubSubCommunicationLinkConfiguration{
			DataSetReaderRef: r, ExpectedSubscribedDataSetVersion: fxVersion(l.ExpectedSubscribedVersion),
			DataSetWriterRef: w, ExpectedPublishedDataSetVersion: fxVersion(l.ExpectedPublishedVersion),
		}
	}
	return cfg, nil
}

func (b *fxRequestBuilder) parameter(field string, e fxEndpointIn) (*uafx.PubSubConnectionEndpointParameter, error) {
	if e.Name == "" {
		return nil, fxRequestErrorf("%s: no name (or node of an existing endpoint)", field)
	}
	mode, err := lookup(field+".mode", "mode", e.Mode, fxModes)
	if err != nil {
		return nil, err
	}
	p := &uafx.PubSubConnectionEndpointParameter{
		Name: e.Name, IsPersistent: e.Persistent, CleanupTimeout: -1,
		IsPreconfigured: e.Preconfigured, Mode: mode,
		ConnectionEndpointTypeID: ua.NewNumericNodeID(b.ac, fxacid.PubSubConnectionEndpointType),
	}
	if e.Type != "" {
		if p.ConnectionEndpointTypeID, err = b.nodeID(field+".type", e.Type); err != nil {
			return nil, err
		}
	}
	if e.CleanupTimeoutMs != nil {
		p.CleanupTimeout = *e.CleanupTimeoutMs
	}
	if p.InputVariableIDs, err = b.nodeIDs(field+".inputs", e.Inputs); err != nil {
		return nil, err
	}
	if p.OutputVariableIDs, err = b.nodeIDs(field+".outputs", e.Outputs); err != nil {
		return nil, err
	}
	if r := e.Related; r != nil {
		p.RelatedEndpoint = &uafx.RelatedEndpoint{Address: r.Address, ConnectionEndpointName: r.Name}
		for _, s := range r.Path {
			q := &ua.PortableQualifiedName{Name: s}
			if uri, name, ok := strings.Cut(s, ";"); ok {
				q = &ua.PortableQualifiedName{NamespaceURI: uri, Name: name}
			}
			p.RelatedEndpoint.ConnectionEndpointPath = append(p.RelatedEndpoint.ConnectionEndpointPath, q)
		}
	}
	return p, nil
}

// build returns the request of uafx for f.
func (b *fxRequestBuilder) build(f *fxRequestFile) (*uafx.EstablishConnectionsRequest, error) {
	mask, err := fxCommandMask(f.Commands)
	if err != nil {
		return nil, err
	}
	req := &uafx.EstablishConnectionsRequest{CommandMask: mask}
	for i, a := range f.AssetVerifications {
		field := fmt.Sprintf("assetVerifications[%d]", i)
		asset, err := b.nodeID(field+".asset", a.Asset)
		if err != nil {
			return nil, err
		}
		mode, err := lookup(field+".mode", "mode", a.Mode, fxAssetModes)
		if err != nil {
			return nil, err
		}
		res, err := lookup(field+".expectedResult", "result", a.ExpectedResult, fxAssetResults)
		if err != nil {
			return nil, err
		}
		av := &uafx.AssetVerification{AssetToVerify: asset, VerificationMode: mode, ExpectedVerificationResult: res}
		for j, kv := range a.Variables {
			f := fmt.Sprintf("%s.variables[%d]", field, j)
			k, err := b.qualifiedName(f+".key", kv.Key)
			if err != nil {
				return nil, err
			}
			v, err := fxValue(f+".value", kv.Type, kv.Value)
			if err != nil {
				return nil, err
			}
			av.ExpectedVerificationVariables = append(av.ExpectedVerificationVariables, &ua.KeyValuePair{Key: k, Value: v})
		}
		if av.ExpectedAdditionalVerificationVariables, err = b.pairs(field+".additionalVariables", a.AdditionalVariables); err != nil {
			return nil, err
		}
		req.AssetVerifications = append(req.AssetVerifications, av)
	}
	for i, e := range f.Endpoints {
		cfg, err := b.endpoint(i, e)
		if err != nil {
			return nil, err
		}
		req.ConnectionEndpointConfigurations = append(req.ConnectionEndpointConfigurations, cfg)
	}
	for _, r := range f.ReserveCommunicationIDs {
		req.ReserveCommunicationIDs = append(req.ReserveCommunicationIDs, &uafx.PubSubReserveCommunicationIDs{
			TransportProfileURI: r.TransportProfileURI, NumReqWriterGroupIDs: r.WriterGroups,
			NumReqDataSetWriterIDs: r.DataSetWriters, RequestTransportSpecificInfo: r.TransportSpecificInfo,
		})
	}
	for i, c := range f.CommunicationConfigurations {
		field := fmt.Sprintf("communicationConfigurations[%d]", i)
		cc := &uafx.PubSubCommunicationConfiguration{RequireCompleteUpdate: c.RequireCompleteUpdate}
		if len(c.PubSubConfiguration) == 0 {
			return nil, fxRequestErrorf("%s: no pubsubConfiguration", field)
		}
		cc.PubSubConfiguration = new(ua.PubSubConfiguration2DataType)
		if err := ua.DecodeJSON(c.PubSubConfiguration, cc.PubSubConfiguration); err != nil {
			return nil, fxRequestErrorf("%s.pubsubConfiguration: %v", field, err)
		}
		for j := range c.References {
			r, err := fxRef(fmt.Sprintf("%s.references[%d]", field, j), &c.References[j])
			if err != nil {
				return nil, err
			}
			cc.ConfigurationReferences = append(cc.ConfigurationReferences, r)
		}
		req.CommunicationConfigurations = append(req.CommunicationConfigurations, cc)
	}
	return req, nil
}
