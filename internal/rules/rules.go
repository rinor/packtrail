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

// Package rules compiles and evaluates the boolean expressions used by choice
// nodes. Expressions are written in expr-lang and evaluated against the shared
// execution payload, exposed as the variable `payload`.
//
//	when: "payload.risk_score > 80"
package rules

import (
	"encoding/json"
	"fmt"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

const payloadKey = "payload"

// Program is a compiled choice expression.
type Program struct {
	src  string
	prog *vm.Program
}

// Compile compiles a boolean expression that may reference `payload`.
func Compile(code string) (*Program, error) {
	prog, err := expr.Compile(code,
		expr.Env(map[string]any{payloadKey: map[string]any{}}),
		expr.AsBool(),
		expr.AllowUndefinedVariables(),
	)
	if err != nil {
		return nil, fmt.Errorf("rules: compile %q: %w", code, err)
	}

	return &Program{src: code, prog: prog}, nil
}

// Match evaluates the program against the JSON payload. A nil/zero result and a
// non-nil error are returned when evaluation fails (e.g. a referenced field is
// missing); callers typically treat that as "no match" and fall through to the
// default rule.
func (p *Program) Match(payload json.RawMessage) (bool, error) {
	var m map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &m); err != nil {
			return false, fmt.Errorf("rules: payload: %w", err)
		}
	}

	out, err := expr.Run(p.prog, map[string]any{payloadKey: m})
	if err != nil {
		return false, fmt.Errorf("rules: eval %q: %w", p.src, err)
	}

	b, ok := out.(bool)
	if !ok {
		return false, fmt.Errorf("rules: %q did not evaluate to bool", p.src)
	}

	return b, nil
}
