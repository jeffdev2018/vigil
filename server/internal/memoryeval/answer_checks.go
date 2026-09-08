package memoryeval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// CodeObservation is produced by the server, never accepted from a runtime.
// Retained outputs let report gates run without executing code again. Imported
// reports remain human-supplied evidence, not cryptographic attestation.
type CodeObservation struct {
	Binding    string          `json:"binding"`
	Diagnostic string          `json:"diagnostic,omitempty"`
	Outputs    json.RawMessage `json:"outputs"`
	Error      bool            `json:"error"`
}

type functionTest struct {
	Args     []json.RawMessage `json:"args"`
	Expected json.RawMessage   `json:"expected"`
}

func checkContract(c ConnectedCase, image string) []byte {
	if c.Check == "" || c.Check == "exact" {
		return []byte(c.Expected)
	}
	if c.Check != "javascript" {
		image = ""
	}
	b, _ := json.Marshal([]string{c.Check, c.Expected, image})
	return b
}

func functionTests(expected string) ([]functionTest, error) {
	var tests []functionTest
	d := json.NewDecoder(strings.NewReader(expected))
	d.DisallowUnknownFields()
	if d.Decode(&tests) != nil || d.Decode(new(any)) != io.EOF || len(tests) == 0 || len(tests) > 16 {
		return nil, errors.New("provide 1–16 JavaScript tests with args and expected")
	}
	for _, test := range tests {
		if test.Args == nil || len(test.Args) > 16 || len(test.Expected) == 0 {
			return nil, errors.New("each JavaScript test requires args and expected")
		}
	}
	return tests, nil
}

func validateAnswerCheck(c ConnectedCase, image string) error {
	switch c.Check {
	case "", "exact":
	case "json":
		if _, err := decodeAnswerJSON(c.Expected); err != nil {
			return errors.New("expected answer must be valid JSON")
		}
	case "javascript":
		if !imageID.MatchString(image) {
			return errors.New("JavaScript checks require a configured immutable Node image")
		}
		_, err := functionTests(c.Expected)
		return err
	default:
		return errors.New("unsupported answer check")
	}
	return nil
}

func decodeAnswerJSON(s string) (any, error) {
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("expected one JSON value")
	}
	return v, nil
}

// Very large numbers retain exact lexical comparison instead of allocating
// arbitrarily large rationals for an untrusted exponent.
func boundedNumber(n json.Number) bool {
	if len(n) > 128 {
		return false
	}
	if i := strings.IndexAny(string(n), "eE"); i >= 0 {
		exp, err := strconv.Atoi(string(n)[i+1:])
		return err == nil && exp >= -4096 && exp <= 4096
	}
	return true
}

func equalJSON(a, b any) bool {
	switch a := a.(type) {
	case json.Number:
		b, ok := b.(json.Number)
		if !ok {
			return false
		}
		// Bound exponents before using arbitrary precision to avoid unbounded work.
		if !boundedNumber(a) || !boundedNumber(b) {
			return a == b
		}
		x, ok := new(big.Rat).SetString(string(a))
		y, yes := new(big.Rat).SetString(string(b))
		return ok && yes && x.Cmp(y) == 0
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, v := range a {
			other, exists := b[k]
			if !exists || !equalJSON(v, other) {
				return false
			}
		}
		return true
	case []any:
		b, ok := b.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i, v := range a {
			if !equalJSON(v, b[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

func codeBinding(c ConnectedCase, artifact, image string) string {
	b, _ := json.Marshal([]string{string(checkContract(c, image)), artifact, "javascript_v1"})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func gradeAnswerCheck(c ConnectedCase, o Outcome, code *CodeObservation, image string) (Outcome, error) {
	if err := validateAnswerCheck(c, image); err != nil {
		return Outcome{}, err
	}
	o.Status, o.Diagnostic = "failed", "Structured JSON check failed"
	if c.Check == "json" {
		a, err := decodeAnswerJSON(o.Artifact)
		b, _ := decodeAnswerJSON(c.Expected)
		if err == nil && equalJSON(a, b) {
			o.Status, o.Diagnostic = "passed", "Structured JSON check passed"
		}
		return o, nil
	}
	if code == nil || code.Binding != codeBinding(c, o.Artifact, image) {
		return Outcome{}, errors.New("missing or mismatched server code observation")
	}
	o.Code = code
	if code.Error {
		o.Status, o.Diagnostic = "error", "Code verifier failed or timed out"
		if code.Diagnostic != "" {
			o.Diagnostic += ": " + code.Diagnostic
		}
		return o, nil
	}
	tests, _ := functionTests(c.Expected)
	var outputs []struct {
		Value json.RawMessage `json:"value"`
		Error bool            `json:"error"`
	}
	if len(code.Outputs) > 64<<10 || json.Unmarshal(code.Outputs, &outputs) != nil || len(outputs) != len(tests) {
		return Outcome{}, errors.New("invalid code observations")
	}
	o.Diagnostic = "JavaScript function tests failed"
	for i, out := range outputs {
		a, err := decodeAnswerJSON(string(out.Value))
		b, _ := decodeAnswerJSON(string(tests[i].Expected))
		if out.Error || err != nil || !equalJSON(a, b) {
			return o, nil
		}
	}
	o.Status, o.Diagnostic = "passed", "JavaScript function tests passed"
	return o, nil
}

// VerifyJavaScript executes only in the existing restricted Docker runner. The
// generated program sees arguments, never expected answers or provider credentials.
func VerifyJavaScript(ctx context.Context, c ConnectedCase, artifact, image string) *CodeObservation {
	o := &CodeObservation{Binding: codeBinding(c, artifact, image), Error: true, Diagnostic: "verifier setup failed"}
	if validateAnswerCheck(c, image) != nil || len(artifact) > 64<<10 {
		return o
	}
	tests, _ := functionTests(c.Expected)
	args := make([][]json.RawMessage, len(tests))
	for i, t := range tests {
		args[i] = t.Args
	}
	input, err := os.MkdirTemp("", "multica-code-check-")
	if err != nil {
		return o
	}
	defer os.RemoveAll(input)
	if os.Chmod(input, 0755) != nil {
		return o
	}
	b, _ := json.Marshal(args)
	const runner = `const fs = require('node:fs');
const args = JSON.parse(fs.readFileSync('/input/args.json', 'utf8'));
(async () => {
  let fn; try { fn = require('/input/answer.cjs'); } catch {}
  const outputs = [];
  for (const input of args) {
    try { outputs.push({value: await fn(...input)}); } catch { outputs.push({error: true}); }
  }
  process.stdout.write(JSON.stringify(outputs));
})().catch(() => process.exit(2));`
	for name, data := range map[string][]byte{"args.json": b, "answer.cjs": []byte(artifact), "runner.cjs": []byte(runner)} {
		if os.WriteFile(filepath.Join(input, name), data, 0644) != nil {
			return o
		}
	}
	stdout, diagnostic, exit, err := container(ctx, Suite{Image: image, TimeoutSeconds: 15}, map[string]string{"/input": input}, []string{"node", "/input/runner.cjs"})
	o.Diagnostic = fmt.Sprintf("exit=%d: %v; %s", exit, err, diagnostic)
	if len(o.Diagnostic) > 2048 {
		o.Diagnostic = o.Diagnostic[:2048]
	}
	if err == nil && exit == 0 && json.Valid([]byte(stdout)) {
		o.Diagnostic = ""
		o.Error = false
		o.Outputs = json.RawMessage(stdout)
		if _, err := gradeAnswerCheck(c, Outcome{Artifact: artifact}, o, image); err != nil {
			o.Error = true
			o.Diagnostic = err.Error()
			o.Outputs = nil
		}
	}
	return o
}
