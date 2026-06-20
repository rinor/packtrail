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

// Command worker is a tiny example task service for the research-pipeline flow.
// It serves every tasks.* subject with an echo handler that records which node
// ran into the shared payload, demonstrating the pkg/protocol contract.
//
//	go run ./examples/worker --nats nats://127.0.0.1:4222
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"

	"github.com/henomis/packtrail/pkg/protocol"
)

func main() {
	url := flag.String("nats", nats.DefaultURL, "NATS server URL")
	namespace := flag.String("namespace", "packtrail", "packtrail namespace to serve [$PACKTRAIL_NAMESPACE]")

	flag.Parse()

	if v := os.Getenv("PACKTRAIL_NAMESPACE"); v != "" && *namespace == "packtrail" {
		*namespace = v
	}

	nc, err := nats.Connect(*url)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	// One wildcard subscription per task type. The trailing "*" matches the
	// execution id segment templated by {execution_id}.
	// ServeNamespaced prepends the namespace so subjects become e.g.
	// "packtrail.tasks.triage.*".
	subjects := []string{
		"tasks.triage.*",
		"tasks.tech-research.*",
		"tasks.market-research.*",
		"tasks.legal-research.*",
		"tasks.synthesis.*",
		"tasks.escalation.*",
	}
	for _, subj := range subjects {
		s := subj
		if _, err := protocol.ServeNamespaced(nc, *namespace, s, echo(s)); err != nil {
			log.Fatalf("serve %s: %v", s, err)
		}
	}

	fmt.Printf("worker serving %d task subjects (namespace %s) on %s\n", len(subjects), *namespace, *url)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
}

// echo returns a handler that marks the node as done in the shared payload and
// echoes it back.
func echo(subject string) protocol.Handler {
	return func(_ context.Context, req protocol.TaskRequest) (protocol.TaskResponse, error) {
		fmt.Printf("[%s] node=%s exec=%s attempt=%d\n", subject, req.NodeID, req.ExecutionID, req.Attempt)

		root := map[string]json.RawMessage{}
		_ = json.Unmarshal(req.Payload, &root)
		root[req.NodeID] = json.RawMessage(`"done"`)

		out, err := json.Marshal(root)
		if err != nil {
			return protocol.TaskResponse{Status: protocol.StatusError, Error: err.Error()}, nil
		}

		return protocol.TaskResponse{Status: protocol.StatusOK, Payload: out}, nil
	}
}
