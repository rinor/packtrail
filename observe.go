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

package packtrail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/henomis/packtrail/internal/dsl"
	"github.com/henomis/packtrail/internal/store"
)

// FlowGraph is the static structure of a flow, for visualisation. It is
// published to a KV registry at startup so observability tools can render a flow
// without its source YAML.
type FlowGraph struct {
	Name  string      `json:"name"`
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// GraphNode is one node of a FlowGraph. Fields are type-specific; empty ones are
// omitted.
type GraphNode struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"` // task | fanout | fanin | choice | signal
	Invoker    string      `json:"invoker,omitempty"`
	Target     string      `json:"target,omitempty"`
	Branches   []string    `json:"branches,omitempty"`
	WaitFor    []string    `json:"wait_for,omitempty"`
	JoinPolicy string      `json:"join_policy,omitempty"`
	Rules      []GraphRule `json:"rules,omitempty"`
	SignalName string      `json:"signal_name,omitempty"`
	OnTimeout  string      `json:"on_timeout,omitempty"`
}

// GraphRule is one routing rule of a choice node.
type GraphRule struct {
	When    string `json:"when,omitempty"`
	Default bool   `json:"default,omitempty"`
	To      string `json:"to"`
}

// GraphEdge is a static edge between two nodes.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Event is a flow execution transition, suitable for a live activity feed.
type Event struct {
	ExecID   string    `json:"exec_id"`
	Flow     string    `json:"flow"`
	Status   string    `json:"status"`
	Node     string    `json:"node"`
	Revision uint64    `json:"revision"`
	Time     time.Time `json:"time"`
}

// buildFlowGraph projects a parsed flow into its public, serialisable graph.
func buildFlowGraph(f *dsl.Flow) FlowGraph {
	g := FlowGraph{Name: f.Name}
	for i := range f.Nodes {
		n := &f.Nodes[i]

		gn := GraphNode{
			ID:         n.ID,
			Type:       n.Type,
			Invoker:    n.Invoker,
			Target:     n.InvokeTarget(),
			Branches:   n.Branches,
			WaitFor:    n.WaitFor,
			JoinPolicy: n.JoinPolicy,
			SignalName: n.SignalName,
			OnTimeout:  n.OnTimeout,
		}
		for _, r := range n.Rules {
			gn.Rules = append(gn.Rules, GraphRule{When: r.When, Default: r.Default, To: r.To})
		}

		g.Nodes = append(g.Nodes, gn)
	}

	for _, e := range f.Edges {
		g.Edges = append(g.Edges, GraphEdge{From: e.From, To: e.To})
	}

	return g
}

// ListFlows returns the names of every flow in the registry. Unlike Flows() (the
// flows this Server instance loaded), this reads the shared KV registry, so an
// observer process that loaded no flows still sees them.
func (s *Server) ListFlows(ctx context.Context) ([]string, error) {
	keys, err := s.flowsKV.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return nil, nil
		}

		return nil, err
	}

	return keys, nil
}

// FlowGraph returns a flow's graph from the registry, or ErrNotFound.
func (s *Server) FlowGraph(ctx context.Context, name string) (*FlowGraph, error) {
	entry, err := s.flowsKV.Get(ctx, name)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	var g FlowGraph

	err = json.Unmarshal(entry.Value(), &g)
	if err != nil {
		return nil, err
	}

	return &g, nil
}

// WatchEvents streams execution transitions as they happen. It delivers events
// published after the call (an ephemeral consumer with DeliverNew); load current
// state via Get/ByStatus first, then apply events live. The channel is closed
// when ctx is cancelled.
func (s *Server) WatchEvents(ctx context.Context) (<-chan Event, error) {
	js := s.store.JS()
	n := s.store.Names()

	cons, err := js.CreateOrUpdateConsumer(ctx, n.StreamEvents, jetstream.ConsumerConfig{
		FilterSubject: n.SubjEventsPrefix + ">",
		DeliverPolicy: jetstream.DeliverNewPolicy,
		AckPolicy:     jetstream.AckNonePolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("events consumer: %w", err)
	}

	const eventChanBuf = 64

	out := make(chan Event, eventChanBuf)

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var ev store.Event
		if unmarshalErr := json.Unmarshal(msg.Data(), &ev); unmarshalErr != nil {
			return
		}

		select {
		case out <- Event{
			ExecID: ev.ExecID, Flow: ev.FlowName, Status: ev.Status,
			Node: ev.Node, Revision: ev.Revision, Time: ev.Time,
		}:
		case <-ctx.Done():
		}
	})
	if err != nil {
		return nil, fmt.Errorf("consume events: %w", err)
	}

	go func() {
		<-ctx.Done()
		cc.Stop()
		close(out)
	}()

	return out, nil
}
