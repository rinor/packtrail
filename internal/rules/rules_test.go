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

package rules

import (
	"encoding/json"
	"testing"
)

func TestMatch(t *testing.T) {
	p, err := Compile("payload.risk_score > 80")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	cases := []struct {
		payload string
		want    bool
	}{
		{`{"risk_score": 90}`, true},
		{`{"risk_score": 50}`, false},
		{`{"risk_score": 80}`, false},
	}
	for _, c := range cases {
		got, matchErr := p.Match(json.RawMessage(c.payload))
		if matchErr != nil {
			t.Errorf("match %s: %v", c.payload, matchErr)
			continue
		}

		if got != c.want {
			t.Errorf("match %s = %v, want %v", c.payload, got, c.want)
		}
	}
}

func TestMatchMissingFieldErrors(t *testing.T) {
	p, _ := Compile("payload.risk_score > 80")
	// Missing field: expr returns an error comparing nil to int; callers treat
	// that as no-match.
	if _, err := p.Match(json.RawMessage(`{}`)); err == nil {
		t.Error("expected error for missing field")
	}
}

func TestCompileInvalid(t *testing.T) {
	if _, err := Compile("payload.x >"); err == nil {
		t.Error("expected compile error")
	}
}
