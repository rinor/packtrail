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

// Package dsl parses and validates Packtrail Flow Definitions (YAML) and exposes
// graph-walk helpers used by the runtime.
package dsl

import (
	"strconv"
	"strings"
)

// Node types.
const (
	NodeTask   = "task"
	NodeFanout = "fanout"
	NodeFanin  = "fanin"
	NodeChoice = "choice"
	NodeSignal = "signal"
)

// DefaultInvoker is the invoker kind used by a task node that does not set one.
// It selects packtrail's built-in NATS request/reply transport (pkg/protocol).
const DefaultInvoker = "nats-task"

// Join policy kinds for a fanin node.
const (
	JoinAll    = "all"
	JoinAny    = "any"
	JoinQuorum = "quorum" // requires Quorum > 0
)

// Flow is a parsed, validated Flow Definition.
type Flow struct {
	Version string `yaml:"version"`
	Name    string `yaml:"name"`
	Nodes   []Node `yaml:"nodes"`
	Edges   []Edge `yaml:"edges"`

	byID    map[string]*Node  // index built by Validate
	next    map[string]string // from -> to, built by Validate
	startID string            // computed by Validate
}

// Node is a single node in the flow graph. Fields are type-specific; Validate
// enforces which are required for each Type.
type Node struct {
	ID   string `yaml:"id"`
	Type string `yaml:"type"`

	// task
	Invoker string       `yaml:"invoker"` // invocation kind; defaults to DefaultInvoker
	Target  string       `yaml:"target"`  // invoker-specific target; defaults to Subject
	Subject string       `yaml:"subject"` // nats-task subject (alias for Target)
	Timeout Duration     `yaml:"timeout"`
	Retry   *RetryPolicy `yaml:"retry"`

	// fanout
	Branches []string `yaml:"branches"`

	// fanin
	WaitFor    []string `yaml:"wait_for"`
	JoinPolicy string   `yaml:"join_policy"`

	// choice
	Rules []Rule `yaml:"rules"`

	// signal
	SignalName string `yaml:"signal_name"`
	OnTimeout  string `yaml:"on_timeout"`
}

// RetryPolicy controls task retries.
type RetryPolicy struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff"` // "exponential" | "linear" | "fixed" | "" (default fixed)
}

// Rule is one branch of a choice node. Exactly one of When / Default is set.
type Rule struct {
	When    string `yaml:"when"`
	Default bool   `yaml:"default"`
	To      string `yaml:"to"`
}

// Edge is a static graph edge.
type Edge struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// JoinKind returns the parsed join policy kind and, for quorum, the required
// count N. Defaults to JoinAll when unset.
func (n *Node) JoinKind() (kind string, quorum int) {
	jp := strings.TrimSpace(n.JoinPolicy)
	switch {
	case jp == "" || jp == JoinAll:
		return JoinAll, 0
	case jp == JoinAny:
		return JoinAny, 0
	case strings.HasPrefix(jp, "quorum:"):
		n, _ := strconv.Atoi(strings.TrimPrefix(jp, "quorum:"))
		return JoinQuorum, n
	default:
		return JoinAll, 0
	}
}

// InvokerKind returns the invoker kind for a task node, defaulting to
// DefaultInvoker ("nats-task") when unset.
func (n *Node) InvokerKind() string {
	if n.Invoker != "" {
		return n.Invoker
	}

	return DefaultInvoker
}

// InvokeTarget returns the invoker-specific target for a task node. Target takes
// precedence; Subject is kept as the alias so existing nats-task flows are
// unchanged.
func (n *Node) InvokeTarget() string {
	if n.Target != "" {
		return n.Target
	}

	return n.Subject
}

// Node returns the node with the given id, or nil.
func (f *Flow) Node(id string) *Node { return f.byID[id] }

// Successor returns the id of the node reached by the single outgoing edge of
// id, or "" if id has no outgoing edge (a terminal node).
func (f *Flow) Successor(id string) string { return f.next[id] }

// StartNode returns the id of the unique node with no inbound transition.
func (f *Flow) StartNode() string { return f.startID }

// ResolvePlaceholders substitutes the {execution_id} placeholder in a task
// node's target (subject, agent name, URL, …) with the concrete execution id.
func ResolvePlaceholders(target, executionID string) string {
	return strings.ReplaceAll(target, "{execution_id}", executionID)
}
