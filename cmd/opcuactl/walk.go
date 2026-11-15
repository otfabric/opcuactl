// SPDX-License-Identifier: MIT

package opcuactl

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/otfabric/go-opcua"
	"github.com/otfabric/go-opcua/ua"
	"github.com/spf13/pflag"
)

// The bounds of a recursive walk of the address space: get vars, browse
// tree, find node, find type, export nodes and export values --start-node.
// References may loop (Part 3 §7.3), and the server decides how many nodes a
// browse returns. A walk visits each node once, stops after --max-nodes
// nodes with the output so far and exit 7, and ends after --walk-timeout
// with exit 5.
const (
	defaultMaxNodes    = 100000
	defaultWalkTimeout = 10 * time.Minute
)

type walkFlags struct {
	maxNodes int
	timeout  time.Duration
}

func (f *walkFlags) register(fs *pflag.FlagSet) {
	fs.IntVar(&f.maxNodes, "max-nodes", defaultMaxNodes, "Stop the walk after this many nodes, with the output so far and exit 7 (0 = no limit)")
	fs.DurationVar(&f.timeout, "walk-timeout", defaultWalkTimeout, "Time limit of the whole walk, exit 5 (0 = no limit)")
}

func (f *walkFlags) validate() error {
	if f.maxNodes < 0 {
		return exitCodef(exitUsage, "--max-nodes %d: 0 or more (0 = no limit)", f.maxNodes)
	}
	if f.timeout < 0 {
		return exitCodef(exitUsage, "--walk-timeout %v: 0 or more (0 = no limit)", f.timeout)
	}
	return nil
}

// context bounds ctx by --walk-timeout.
func (f *walkFlags) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if f.timeout == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, f.timeout)
}

// nodeLimitError ends a walk that reached --max-nodes.
type nodeLimitError struct{ max int }

func (e *nodeLimitError) Error() string {
	return fmt.Sprintf("stopped after %d nodes (--max-nodes): the output is incomplete", e.max)
}

// partialWalk returns the exit 7 of a walk that err says reached --max-nodes,
// nil for another error.
func partialWalk(what string, err error) error {
	var le *nodeLimitError
	if errors.As(err, &le) {
		return exitCodef(exitPartial, "%s: %v", what, le)
	}
	return nil
}

// walk is the walk of n with WalkLimitDedup: each node once, up to maxDepth
// (the WalkLimit convention). After --max-nodes nodes it yields a
// *nodeLimitError and ends.
func (f *walkFlags) walk(ctx context.Context, n *opcua.Node, maxDepth int) iter.Seq2[opcua.WalkResult, error] {
	return func(yield func(opcua.WalkResult, error) bool) {
		nodes := 0
		for wr, err := range n.WalkLimitDedup(ctx, maxDepth) {
			if err == nil {
				if f.maxNodes > 0 && nodes >= f.maxNodes {
					yield(opcua.WalkResult{}, &nodeLimitError{max: f.maxNodes})
					return
				}
				nodes++
			}
			if !yield(wr, err) {
				return
			}
		}
	}
}

// treeWalk is the state of the walk of browse tree in JSON and YAML: the
// nodes seen, and how many.
type treeWalk struct {
	c       *opcua.Client
	max     int
	nodes   int
	visited map[string]bool
}

func (f *walkFlags) treeWalk(c *opcua.Client, root *ua.NodeID) *treeWalk {
	w := &treeWalk{c: c, max: f.maxNodes, visited: map[string]bool{}}
	if root != nil {
		w.visited[root.String()] = true
	}
	return w
}

// add reports whether ref is a node not seen before, and counts it. It
// returns a *nodeLimitError when --max-nodes nodes were counted already.
func (w *treeWalk) add(ref *ua.ReferenceDescription) (bool, error) {
	key := ""
	if ref.NodeID != nil && ref.NodeID.NodeID != nil {
		key = ref.NodeID.NodeID.String()
	}
	if key != "" && w.visited[key] {
		return false, nil
	}
	if w.max > 0 && w.nodes >= w.max {
		return false, &nodeLimitError{max: w.max}
	}
	if key != "" {
		w.visited[key] = true
	}
	w.nodes++
	return true, nil
}
