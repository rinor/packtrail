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

import "testing"

// TestExampleFlowsValidate ensures every flow shipped under examples/ parses and
// passes validation, so the CLI's default flows directory always works.
func TestExampleFlowsValidate(t *testing.T) {
	flows, err := LoadDir("../../examples")
	if err != nil {
		t.Fatalf("load example flows: %v", err)
	}

	if len(flows) == 0 {
		t.Fatal("no example flows found")
	}

	for name, f := range flows {
		if f.StartNode() == "" {
			t.Errorf("flow %q has no start node", name)
		}
	}
}
