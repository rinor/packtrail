// Copyright 2026 Simone Vellei
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dsl

import (
	"fmt"
	"strings"
)

// Validate checks structural and semantic correctness of the flow and builds
// the internal indexes used by the graph-walk helpers. It is called by Parse.
//
//nolint:gocognit,funlen
func (f *Flow) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("flow: missing name")
	}

	if len(f.Nodes) == 0 {
		return fmt.Errorf("flow %q: no nodes", f.Name)
	}

	// Index nodes, checking for duplicates.
	f.byID = make(map[string]*Node, len(f.Nodes))
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if n.ID == "" {
			return fmt.Errorf("flow %q: node with empty id", f.Name)
		}

		if _, dup := f.byID[n.ID]; dup {
			return fmt.Errorf("flow %q: duplicate node id %q", f.Name, n.ID)
		}

		f.byID[n.ID] = n
	}

	// Per-type field validation.
	for i := range f.Nodes {
		if err := f.validateNode(&f.Nodes[i]); err != nil {
			return err
		}
	}

	// Build the edge map; an explicit edge defines the single successor of a node.
	f.next = make(map[string]string, len(f.Edges))
	inbound := make(map[string]bool)

	for _, e := range f.Edges {
		if f.byID[e.From] == nil {
			return fmt.Errorf("flow %q: edge from unknown node %q", f.Name, e.From)
		}

		if f.byID[e.To] == nil {
			return fmt.Errorf("flow %q: edge to unknown node %q", f.Name, e.To)
		}

		if _, dup := f.next[e.From]; dup {
			return fmt.Errorf("flow %q: node %q has more than one outgoing edge", f.Name, e.From)
		}

		f.next[e.From] = e.To
		inbound[e.To] = true
	}

	// Mark targets reachable only via node-internal transitions as inbound, so
	// they are not mistaken for start nodes (fanout branches, choice/signal targets).
	for i := range f.Nodes {
		n := &f.Nodes[i]
		switch n.Type {
		case NodeFanout:
			for _, b := range n.Branches {
				inbound[b] = true
			}
		case NodeFanin:
			for _, w := range n.WaitFor {
				inbound[w] = true
			}
		case NodeChoice:
			for _, r := range n.Rules {
				inbound[r.To] = true
			}
		case NodeSignal:
			if n.OnTimeout != "" {
				inbound[n.OnTimeout] = true
			}
		}
	}

	// Determine the unique start node.
	var starts []string

	for i := range f.Nodes {
		if !inbound[f.Nodes[i].ID] {
			starts = append(starts, f.Nodes[i].ID)
		}
	}

	switch len(starts) {
	case 0:
		return fmt.Errorf("flow %q: no start node (every node has an inbound transition)", f.Name)
	case 1:
		f.startID = starts[0]
	default:
		return fmt.Errorf("flow %q: multiple start nodes %v (exactly one required)", f.Name, starts)
	}

	return nil
}

//nolint:gocognit,funlen
func (f *Flow) validateNode(n *Node) error {
	ref := func(id, field string) error {
		if id == "" {
			return fmt.Errorf("flow %q: node %q: %s is required", f.Name, n.ID, field)
		}

		if f.byID[id] == nil {
			return fmt.Errorf("flow %q: node %q: %s references unknown node %q", f.Name, n.ID, field, id)
		}

		return nil
	}

	switch n.Type {
	case NodeTask:
		if strings.TrimSpace(n.Subject) == "" && strings.TrimSpace(n.Target) == "" {
			return fmt.Errorf("flow %q: task node %q: subject or target is required", f.Name, n.ID)
		}

		if n.Retry != nil && n.Retry.MaxAttempts < 0 {
			return fmt.Errorf("flow %q: task node %q: retry.max_attempts must be >= 0", f.Name, n.ID)
		}
	case NodeFanout:
		if len(n.Branches) == 0 {
			return fmt.Errorf("flow %q: fanout node %q: branches is required", f.Name, n.ID)
		}

		for _, b := range n.Branches {
			if err := ref(b, "branch"); err != nil {
				return err
			}
		}
	case NodeFanin:
		if len(n.WaitFor) == 0 {
			return fmt.Errorf("flow %q: fanin node %q: wait_for is required", f.Name, n.ID)
		}

		for _, w := range n.WaitFor {
			if err := ref(w, "wait_for"); err != nil {
				return err
			}
		}

		kind, quorum := n.JoinKind()
		if kind == JoinQuorum && (quorum <= 0 || quorum > len(n.WaitFor)) {
			return fmt.Errorf("flow %q: fanin node %q: quorum:N must satisfy 0 < N <= len(wait_for)", f.Name, n.ID)
		}
	case NodeChoice:
		if len(n.Rules) == 0 {
			return fmt.Errorf("flow %q: choice node %q: at least one rule is required", f.Name, n.ID)
		}

		sawDefault := false

		for _, r := range n.Rules {
			if r.Default {
				sawDefault = true
			} else if strings.TrimSpace(r.When) == "" {
				return fmt.Errorf("flow %q: choice node %q: non-default rule needs a when expression", f.Name, n.ID)
			}

			if err := ref(r.To, "rule.to"); err != nil {
				return err
			}
		}

		if !sawDefault {
			return fmt.Errorf("flow %q: choice node %q: a default rule is required", f.Name, n.ID)
		}
	case NodeSignal:
		if strings.TrimSpace(n.SignalName) == "" {
			return fmt.Errorf("flow %q: signal node %q: signal_name is required", f.Name, n.ID)
		}

		if n.OnTimeout != "" {
			if err := ref(n.OnTimeout, "on_timeout"); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("flow %q: node %q: unknown type %q", f.Name, n.ID, n.Type)
	}

	return nil
}
