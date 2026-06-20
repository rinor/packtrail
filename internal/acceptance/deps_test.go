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

// Package acceptance holds module-level acceptance checks from spec §12.
package acceptance

import (
	"os"
	"strings"
	"testing"
)

// TestNoForbiddenDependencies enforces the spec §12 acceptance criterion that
// Packtrail must not depend on the integration repository: it is a
// standalone engine backed only by NATS.
func TestNoForbiddenDependencies(t *testing.T) {
	for _, f := range []string{"../../go.mod", "../../go.sum"} {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}

		text := strings.ToLower(string(data))
		for _, forbidden := range []string{"integration"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden dependency %q", f, forbidden)
			}
		}
	}
}
