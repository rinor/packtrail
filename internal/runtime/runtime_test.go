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

package runtime

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/henomis/packtrail/internal/dsl"
	"github.com/henomis/packtrail/internal/names"
	"github.com/henomis/packtrail/internal/natstest"
	"github.com/henomis/packtrail/internal/scheduler"
	"github.com/henomis/packtrail/internal/store"
	"github.com/henomis/packtrail/invoker/natstask"
	"github.com/henomis/packtrail/pkg/protocol"
)

// harness wires an embedded server, store, scheduler and a running engine.
type harness struct {
	nc     *nats.Conn
	prefix string
	store  *store.Store
	engine *Engine
}

func newHarness(t *testing.T, flowYAML string, cfg Config) *harness {
	t.Helper()

	ctx := context.Background()
	srv := natstest.Start(t)
	n := names.New("")

	st, err := store.Open(ctx, srv.JS, n)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	sch, err := scheduler.New(ctx, srv.JS, n)
	if err != nil {
		t.Fatalf("scheduler: %v", err)
	}

	flow, err := dsl.Parse([]byte(flowYAML))
	if err != nil {
		t.Fatalf("flow: %v", err)
	}

	eng, err := New(natstask.New(srv.NC, n.Prefix), st, sch, map[string]*dsl.Flow{flow.Name: flow}, cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)

	go func() { _ = eng.Run(runCtx) }()

	return &harness{nc: srv.NC, prefix: n.Prefix, store: st, engine: eng}
}

// serve registers a task handler for subject (wildcard ok). The namespace
// prefix is prepended automatically to match the invoker's subject convention.
func (h *harness) serve(t *testing.T, subject string, fn protocol.Handler) {
	t.Helper()

	sub, err := protocol.ServeNamespaced(h.nc, h.prefix, subject, fn)
	if err != nil {
		t.Fatalf("serve %s: %v", subject, err)
	}

	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

// waitStatus polls until the execution reaches status, or fails.
func (h *harness) waitStatus(t *testing.T, id, status string, within time.Duration) *store.Execution {
	t.Helper()

	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		ex, err := h.store.Get(context.Background(), id)
		if err == nil && ex.Status == status {
			return ex
		}

		time.Sleep(20 * time.Millisecond)
	}

	ex, _ := h.store.Get(context.Background(), id)
	if ex != nil {
		t.Fatalf("exec %s: status=%q err=%q, want %q", id, ex.Status, ex.Error, status)
	}

	t.Fatalf("exec %s never reached %q", id, status)

	return nil
}

func setField(payload json.RawMessage, key string, val any) json.RawMessage {
	m := map[string]any{}
	_ = json.Unmarshal(payload, &m)
	m[key] = val
	out, _ := json.Marshal(m) //nolint:errchkjson // map[string]any is safe in test helpers

	return out
}

const linearFlow = `
name: linear
nodes:
  - {id: a, type: task, subject: "tasks.a.{execution_id}"}
  - {id: b, type: task, subject: "tasks.b.{execution_id}"}
edges:
  - {from: a, to: b}
`

func TestLinearExecution(t *testing.T) {
	h := newHarness(t, linearFlow, Config{})
	h.serve(t, "tasks.a.*", func(_ context.Context, req protocol.TaskRequest) (protocol.TaskResponse, error) {
		return protocol.TaskResponse{Status: protocol.StatusOK, Payload: setField(req.Payload, "a", true)}, nil
	})
	h.serve(t, "tasks.b.*", func(_ context.Context, req protocol.TaskRequest) (protocol.TaskResponse, error) {
		return protocol.TaskResponse{Status: protocol.StatusOK, Payload: setField(req.Payload, "b", true)}, nil
	})

	id, err := h.engine.Start(context.Background(), "linear", json.RawMessage(`{"start":true}`))
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	ex := h.waitStatus(t, id, store.StatusCompleted, 5*time.Second)

	var got map[string]any

	_ = json.Unmarshal(ex.Payload, &got)
	if got["a"] != true || got["b"] != true || got["start"] != true {
		t.Fatalf("payload not threaded through both tasks: %v", got)
	}
}

const retryFlow = `
name: retry
nodes:
  - id: a
    type: task
    subject: tasks.a.{execution_id}
    retry:
      max_attempts: 3
      backoff: exponential
`

func TestTaskRetry(t *testing.T) {
	h := newHarness(t, retryFlow, Config{RetryBaseDelay: 100 * time.Millisecond})

	var calls int32

	h.serve(t, "tasks.a.*", func(_ context.Context, req protocol.TaskRequest) (protocol.TaskResponse, error) {
		n := atomic.AddInt32(&calls, 1)
		if req.Attempt != int(n)-1 {
			t.Errorf("attempt = %d, want %d", req.Attempt, n-1)
		}

		if n < 3 {
			return protocol.TaskResponse{Status: protocol.StatusRetry, Error: "not yet"}, nil
		}

		return protocol.TaskResponse{Status: protocol.StatusOK, Payload: req.Payload}, nil
	})

	id, err := h.engine.Start(context.Background(), "retry", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	h.waitStatus(t, id, store.StatusCompleted, 5*time.Second)

	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("task called %d times, want 3", got)
	}
}

func TestTaskPermanentError(t *testing.T) {
	h := newHarness(t, retryFlow, Config{RetryBaseDelay: 50 * time.Millisecond})
	h.serve(t, "tasks.a.*", func(_ context.Context, _ protocol.TaskRequest) (protocol.TaskResponse, error) {
		return protocol.TaskResponse{Status: protocol.StatusError, Error: "boom"}, nil
	})

	id, err := h.engine.Start(context.Background(), "retry", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	ex := h.waitStatus(t, id, store.StatusFailed, 5*time.Second)
	if ex.Error == "" {
		t.Fatal("expected error message on failed execution")
	}
}
