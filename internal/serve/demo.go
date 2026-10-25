// SPDX-License-Identifier: MIT

package serve

import (
	"context"
	"crypto/rand"
	"math"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/server"
	"github.com/otfabric/go-opcua/server/attrs"
	"github.com/otfabric/go-opcua/ua"
)

// DemoNamespaceURI is the namespace of the demo address space.
const DemoNamespaceURI = "urn:otfabric:opcuactl:demo"

// The demo address space: string NodeIds in the demo namespace.
const (
	DemoFolder     = "Demo"            // the object all demo nodes are components of; event history of its events
	DemoBoolean    = "Demo.Boolean"    // Boolean, read-only
	DemoInt32      = "Demo.Int32"      // Int32, read-only
	DemoDouble     = "Demo.Double"     // Double, read-only
	DemoString     = "Demo.String"     // String, read-only
	DemoDateTime   = "Demo.DateTime"   // DateTime (start time), read-only
	DemoInt32Array = "Demo.Int32Array" // Int32[5], read-only
	DemoCounter    = "Demo.Counter"    // UInt32, counts up every interval
	DemoSine       = "Demo.Sine"       // Double, a sine wave, historized
	DemoLevel      = "Demo.Level"      // Double, writable, source of DemoAlarm
	DemoAlarm      = "Demo.LevelAlarm" // ExclusiveLevelAlarm on DemoLevel
	DemoAdd        = "Demo.Add"        // method Add(a, b Int32) -> sum Int32
)

// Demo alarm limits of DemoLevel.
const (
	DemoLevelHigh     = 80.0
	DemoLevelHighHigh = 95.0
	// DemoStartedSeverity is the Severity of the event Demo reports at
	// start.
	DemoStartedSeverity = 100
	demoLevelStart      = 50.0
	// demoSinePeriod is the number of intervals of one sine period.
	demoSinePeriod = 60
	// demoHistory is the number of samples of DemoSine the historian keeps.
	demoHistory = 10000
	// demoEventHistory is the number of events of DemoFolder the historian
	// keeps.
	demoEventHistory = 1000
)

type demo struct {
	srv     *server.Server
	ns      *server.NodeNameSpace
	hist    *server.Historian
	counter atomic.Pointer[ua.DataValue]
	sine    atomic.Pointer[ua.DataValue]
	level   *server.Node
	alarm   *server.Condition
}

func (d *demo) nodeID(name string) *ua.NodeID { return ua.NewStringNodeID(d.ns.ID(), name) }

func newDemo(srv *server.Server) (*demo, error) {
	root, err := srv.Namespace(0)
	if err != nil {
		return nil, err
	}
	d := &demo{srv: srv, ns: server.NewNodeNameSpace(srv, DemoNamespaceURI), hist: server.NewHistorian()}
	folder := server.NewFolderNode(d.nodeID(DemoFolder), DemoFolder)
	folder.SetDescription("opcuactl serve demo nodes", "")
	d.qualify(folder, DemoFolder)
	d.ns.AddNode(folder)
	root.Objects().AddRef(folder, id.Organizes, true)

	readOnly := byte(ua.AccessLevelTypeCurrentRead)
	add := func(name string, dataType uint32, value any, access byte) *server.Node {
		n := server.NewVariableNode(d.nodeID(name), name[len(DemoFolder)+1:], value)
		d.qualify(n, name[len(DemoFolder)+1:])
		_ = n.SetAttribute(ua.AttributeIDDataType, server.DataValueFromValue(ua.NewNumericExpandedNodeID(0, dataType)))
		valueRank := int32(-1)
		if name == DemoInt32Array {
			valueRank = 1
		}
		_ = n.SetAttribute(ua.AttributeIDValueRank, server.DataValueFromValue(valueRank))
		_ = n.SetAttribute(ua.AttributeIDAccessLevel, server.DataValueFromValue(access))
		_ = n.SetAttribute(ua.AttributeIDUserAccessLevel, server.DataValueFromValue(access))
		d.ns.AddNode(n)
		folder.AddRef(n, id.HasComponent, true)
		if typ := srv.Node(ua.NewNumericNodeID(0, id.BaseDataVariableType)); typ != nil {
			n.AddRef(typ, id.HasTypeDefinition, true)
		}
		return n
	}
	started := time.Now().UTC().Truncate(time.Millisecond)
	add(DemoBoolean, id.Boolean, true, readOnly)
	add(DemoInt32, id.Int32, int32(42), readOnly)
	add(DemoDouble, id.Double, 3.14, readOnly)
	add(DemoString, id.String, "hello from opcuactl", readOnly)
	add(DemoDateTime, id.DateTime, started, readOnly)
	add(DemoInt32Array, id.Int32, []int32{1, 2, 3, 4, 5}, readOnly)

	d.counter.Store(sample(uint32(0), started))
	d.sine.Store(sample(0.0, started))
	add(DemoCounter, id.UInt32, func() *ua.DataValue { return d.counter.Load() }, readOnly)
	sine := add(DemoSine, id.Double, func() *ua.DataValue { return d.sine.Load() },
		readOnly|byte(ua.AccessLevelTypeHistoryRead))
	_ = sine.SetAttribute(ua.AttributeIDHistorizing, server.DataValueFromValue(true))
	d.hist.EnableNode(sine.ID(), demoHistory)
	d.hist.RecordValue(sine.ID(), d.sine.Load())
	srv.SetHistorian(d.hist)

	d.level = add(DemoLevel, id.Double, demoLevelStart, readOnly|byte(ua.AccessLevelTypeCurrentWrite))

	hi, hihi := DemoLevelHigh, DemoLevelHighHigh
	if d.alarm, err = srv.AddCondition(server.ConditionConfig{
		Type:     ua.NewNumericNodeID(0, id.ExclusiveLevelAlarmType),
		NodeID:   d.nodeID(DemoAlarm),
		Name:     "LevelAlarm",
		Source:   folder.ID(),
		Severity: 700,
		Message:  "Demo.Level is high",
		Shelving: true,
		Limits:   server.AlarmLimits{High: &hi, HighHigh: &hihi},
	}); err != nil {
		return nil, err
	}

	// Demo is a HistoricalEventNode (Part 11 §5.4): the events reported on
	// it, the LevelAlarm transitions and the start event, are kept and may
	// be inserted, replaced and deleted with HistoryUpdate.
	d.hist.EnableEvents(folder.ID(), demoEventHistory)
	if err := srv.EnableEventHistory(folder.ID(), server.EventHistoryOptions{Writable: true}); err != nil {
		return nil, err
	}

	d.addMethod(folder)
	return d, nil
}

// emitStarted reports the start of the server as an event of Demo, so its
// event history is never empty.
func (d *demo) emitStarted() {
	eventID := make([]byte, 16)
	_, _ = rand.Read(eventID)
	_ = d.srv.EmitBaseEvent(d.nodeID(DemoFolder), &server.BaseEvent{
		EventID:    eventID,
		EventType:  ua.NewNumericNodeID(0, id.BaseEventType),
		SourceNode: d.nodeID(DemoFolder),
		SourceName: DemoFolder,
		Time:       time.Now().UTC(),
		Message:    ua.NewLocalizedText("opcuactl serve started"),
		Severity:   DemoStartedSeverity,
	})
}

// addMethod adds Demo.Add(a, b Int32) -> sum Int32 with its argument
// properties.
func (d *demo) addMethod(folder *server.Node) {
	m := server.NewFolderNode(d.nodeID(DemoAdd), "Add")
	m.SetNodeClass(ua.NodeClassMethod)
	m.SetDescription("Returns a + b", "")
	d.qualify(m, "Add")
	d.ns.AddNode(m)
	folder.AddRef(m, id.HasComponent, true)
	arg := func(name, desc string) *ua.Argument {
		return &ua.Argument{Name: name, DataType: ua.NewNumericNodeID(0, id.Int32), ValueRank: -1, Description: ua.NewLocalizedText(desc)}
	}
	d.addArguments(m, "InputArguments", arg("a", "first addend"), arg("b", "second addend"))
	d.addArguments(m, "OutputArguments", arg("sum", "a + b"))
	d.srv.RegisterMethod(folder.ID(), m.ID(), func(_ context.Context, _, _ *ua.NodeID, args []*ua.Variant) ([]*ua.Variant, ua.StatusCode) {
		switch {
		case len(args) < 2:
			return nil, ua.StatusBadArgumentsMissing
		case len(args) > 2:
			return nil, ua.StatusBadTooManyArguments
		}
		a, okA := args[0].Value().(int32)
		b, okB := args[1].Value().(int32)
		if !okA || !okB {
			return nil, ua.StatusBadTypeMismatch
		}
		return []*ua.Variant{ua.MustVariant(a + b)}, ua.StatusOK
	})
}

func (d *demo) addArguments(method *server.Node, name string, args ...*ua.Argument) {
	eos := make([]*ua.ExtensionObject, len(args))
	for i, a := range args {
		eos[i] = ua.NewExtensionObject(a)
	}
	value := &ua.DataValue{EncodingMask: ua.DataValueValue, Value: ua.MustVariant(eos)}
	n := server.NewNode(
		ua.NewStringNodeID(d.ns.ID(), DemoAdd+"."+name),
		map[ua.AttributeID]*ua.DataValue{
			ua.AttributeIDNodeClass:   server.DataValueFromValue(uint32(ua.NodeClassVariable)),
			ua.AttributeIDBrowseName:  server.DataValueFromValue(attrs.BrowseName(name)),
			ua.AttributeIDDisplayName: server.DataValueFromValue(attrs.DisplayName(name, "")),
			ua.AttributeIDDataType:    server.DataValueFromValue(ua.NewNumericExpandedNodeID(0, id.Argument)),
			ua.AttributeIDValueRank:   server.DataValueFromValue(int32(1)),
		},
		nil,
		func() *ua.DataValue { return value },
	)
	d.ns.AddNode(n)
	method.AddRef(n, id.HasProperty, true)
}

// qualify puts the BrowseName of n into the demo namespace.
func (d *demo) qualify(n *server.Node, name string) {
	_ = n.SetAttribute(ua.AttributeIDBrowseName, server.DataValueFromValue(&ua.QualifiedName{NamespaceIndex: d.ns.ID(), Name: name}))
}

func sample(v any, t time.Time) *ua.DataValue {
	return &ua.DataValue{
		EncodingMask:    ua.DataValueValue | ua.DataValueSourceTimestamp | ua.DataValueServerTimestamp,
		Value:           ua.MustVariant(v),
		SourceTimestamp: t,
		ServerTimestamp: t,
	}
}

// run updates the simulated values every interval until stop is closed:
// Counter counts, Sine moves along its wave (and is recorded in the
// history) and LevelAlarm follows Level.
func (d *demo) run(interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	level := math.NaN()
	var tick uint32
	for {
		d.evaluateLevel(&level)
		select {
		case <-stop:
			return
		case <-t.C:
		}
		tick++
		now := time.Now().UTC()
		d.counter.Store(sample(tick, now))
		d.srv.ChangeNotification(d.nodeID(DemoCounter))
		s := sample(math.Round(100*math.Sin(2*math.Pi*float64(tick%demoSinePeriod)/demoSinePeriod))/100, now)
		d.sine.Store(s)
		d.hist.RecordValue(d.nodeID(DemoSine), s)
		d.srv.ChangeNotification(d.nodeID(DemoSine))
	}
}

// evaluateLevel evaluates the alarm limits when Level changed since last.
func (d *demo) evaluateLevel(last *float64) {
	dv := d.level.Value()
	if dv == nil || dv.Value == nil {
		return
	}
	v, ok := dv.Value.Value().(float64)
	if !ok || v == *last {
		return
	}
	*last = v
	_ = d.alarm.EvaluateLimits(v)
}
