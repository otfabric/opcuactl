// SPDX-License-Identifier: MIT

package opcuactl

import (
	"fmt"

	"github.com/otfabric/go-opcua/id"
	"github.com/otfabric/go-opcua/ua"
)

// buildEventFilter constructs an EventFilter with SelectClauses and an optional
// WhereClause combining OfType and Severity bounds with And.
func buildEventFilter(fields []string, ofType *ua.NodeID, sevMin, sevMax *uint16) *ua.EventFilter {
	b := ua.NewEventFilter().Select(fields...)

	var leaves []*ua.ContentFilterElement
	if ofType != nil {
		leaves = append(leaves, ua.OfType(ofType))
	}
	if sevMin != nil {
		leaves = append(leaves, ua.Field("Severity").GreaterThanOrEqual(*sevMin))
	}
	if sevMax != nil {
		leaves = append(leaves, ua.Field("Severity").LessThanOrEqual(*sevMax))
	}
	if len(leaves) == 0 {
		return b.Build()
	}
	if len(leaves) == 1 {
		return b.Where(leaves[0]).Build()
	}

	// Part 4 And takes exactly two operands; nest for 3+ leaves.
	f := b.Build()
	f.WhereClause = &ua.ContentFilter{Elements: andElements(leaves)}
	return f
}

func andElements(leaves []*ua.ContentFilterElement) []*ua.ContentFilterElement {
	if len(leaves) == 2 {
		return []*ua.ContentFilterElement{
			{
				FilterOperator: ua.FilterOperatorAnd,
				FilterOperands: []*ua.ExtensionObject{elementOperand(1), elementOperand(2)},
			},
			leaves[0],
			leaves[1],
		}
	}
	// And(leaf0, And(leaf1, leaf2, ...)) via nested binary And.
	rest := andElements(leaves[1:])
	// Shift rest indices by +2 (slots 0=root And, 1=leaf0, 2..=rest).
	shifted := shiftElementRefs(rest, 2)
	out := make([]*ua.ContentFilterElement, 0, 2+len(shifted))
	out = append(out, &ua.ContentFilterElement{
		FilterOperator: ua.FilterOperatorAnd,
		FilterOperands: []*ua.ExtensionObject{elementOperand(1), elementOperand(2)},
	})
	out = append(out, leaves[0])
	out = append(out, shifted...)
	return out
}

func shiftElementRefs(elements []*ua.ContentFilterElement, delta uint32) []*ua.ContentFilterElement {
	out := make([]*ua.ContentFilterElement, len(elements))
	for i, el := range elements {
		cp := *el
		if el.FilterOperator == ua.FilterOperatorAnd || el.FilterOperator == ua.FilterOperatorOr {
			ops := make([]*ua.ExtensionObject, len(el.FilterOperands))
			for j, op := range el.FilterOperands {
				if eo, ok := op.Value.(*ua.ElementOperand); ok {
					ops[j] = elementOperand(eo.Index + delta)
				} else if eo, ok := op.Value.(ua.ElementOperand); ok {
					ops[j] = elementOperand(eo.Index + delta)
				} else {
					ops[j] = op
				}
			}
			cp.FilterOperands = ops
		}
		out[i] = &cp
	}
	return out
}

func elementOperand(index uint32) *ua.ExtensionObject {
	return &ua.ExtensionObject{
		EncodingMask: ua.ExtensionObjectBinary,
		TypeID:       ua.NewNumericExpandedNodeID(0, id.ElementOperandEncodingDefaultBinary),
		Value:        &ua.ElementOperand{Index: index},
	}
}

// aggregateNodeID maps a CLI aggregate name to the standard NodeId.
// Supported: average, minimum|min, maximum|max, count.
func aggregateNodeID(name string) (*ua.NodeID, error) {
	switch name {
	case "min":
		name = "minimum"
	case "max":
		name = "maximum"
	}
	n, ok := id.AggregateType(name)
	if !ok {
		return nil, fmt.Errorf("unknown --aggregate %q (use average|min|max|count)", name)
	}
	return ua.NewNumericNodeID(0, n), nil
}
