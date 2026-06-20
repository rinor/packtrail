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
	"testing"
	"time"

	"github.com/henomis/packtrail/pkg/protocol"
)

// TestScheduleFlowCron verifies a recurring cron schedule auto-starts executions.
func TestScheduleFlowCron(t *testing.T) {
	h := newHarness(t, linearFlow, Config{})
	started := make(chan string, 4)

	h.serve(t, "tasks.a.*", func(_ context.Context, req protocol.TaskRequest) (protocol.TaskResponse, error) {
		started <- req.ExecutionID
		return protocol.TaskResponse{Status: protocol.StatusOK, Payload: req.Payload}, nil
	})
	h.serve(t, "tasks.b.*", passthrough)

	// Every second (6-field cron: sec min hour dom mon dow).
	if err := h.engine.ScheduleFlow(context.Background(), "tick", "linear", "* * * * * *", json.RawMessage(`{"cron":true}`)); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	select {
	case <-started:
		// An execution was auto-started by the cron schedule.
	case <-time.After(5 * time.Second):
		t.Fatal("cron did not start an execution within 5s")
	}
}
