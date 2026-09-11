// Package memoryeval compares a frozen memory against a baseline using offline
// workers and independent, human-owned executable checks. It never calls providers.
package memoryeval

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Memory struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agent_id"`
	WorkspaceID string     `json:"workspace_id"`
	Content     string     `json:"content"`
	Revision    int32      `json:"revision"`
	Status      string     `json:"status"`
	Expired     bool       `json:"expired"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type Case struct {
	ID     string `json:"id"`
	Split  string `json:"split"`  // replay or holdout; frozen before any execution
	Input  string `json:"input"`  // fixture directory; never a live worktree
	Checks string `json:"checks"` // hidden from worker, mounted only in verifier
}

type Suite struct {
	CodeImage      string           `json:"code_image,omitempty"`
	Image          string           `json:"image"` // immutable local sha256 image ID, no implicit pull
	Worker         []string         `json:"worker"`
	Verifier       []string         `json:"verifier"`
	TimeoutSeconds int              `json:"timeout_seconds"`
	Cases          []Case           `json:"cases"`
	WorkerProtocol string           `json:"worker_protocol,omitempty"` // empty: stdout artifact; multica_runtime_v1: structured adapter result
	Connected      *ConnectedConfig `json:"connected,omitempty"`
	TextCases      []ConnectedCase  `json:"text_cases,omitempty"`
}

type Outcome struct {
	Code               *CodeObservation `json:"code,omitempty"`
	Status             string           `json:"status"` // passed, failed, error
	DurationMS         int64            `json:"duration_ms"`
	Artifact           string           `json:"artifact"`
	Diagnostic         string           `json:"diagnostic"`
	CostUSD            *float64         `json:"cost_usd"`              // unavailable, not zero
	CostSource         string           `json:"cost_source,omitempty"` // catalog_estimate when CostUSD is Multica catalog pricing
	HumanInterventions *int             `json:"human_interventions"`   // not instrumented
	Runtime            *RuntimeEvidence `json:"runtime,omitempty"`
}

type Comparison struct {
	ID         string  `json:"id"`
	Split      string  `json:"split"`
	InputHash  string  `json:"input_hash"`
	ChecksHash string  `json:"checks_hash"`
	Baseline   Outcome `json:"baseline"`
	Candidate  Outcome `json:"candidate"`
}

type Report struct {
	Version     int          `json:"version"`
	Kind        string       `json:"kind"`
	ServerURL   string       `json:"server_url"`
	Candidate   Memory       `json:"candidate"`
	Baseline    []Memory     `json:"baseline"`
	Suite       Suite        `json:"suite"`
	StartedAt   time.Time    `json:"started_at"`
	CompletedAt *time.Time   `json:"completed_at"`
	Cases       []Comparison `json:"cases"`
	Eligible    bool         `json:"eligible"`
	Reason      string       `json:"reason"`
}

var imageID = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var caseID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func ReadJSON(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 64<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON document")
	}
	return nil
}

func (s Suite) Validate() error {
	if s.WorkerProtocol == ConnectedProtocol {
		return s.validateConnected()
	}
	if s.Connected != nil || len(s.TextCases) != 0 || s.CodeImage != "" {
		return errors.New("connected cases require connected worker protocol")
	}
	if s.WorkerProtocol != "" && s.WorkerProtocol != "multica_runtime_v1" {
		return errors.New("unsupported worker_protocol")
	}
	if !imageID.MatchString(s.Image) {
		return errors.New("image must be a full local sha256 image ID")
	}
	if len(s.Worker) == 0 || len(s.Verifier) == 0 || !strings.HasPrefix(s.Worker[0], "/") || !strings.HasPrefix(s.Verifier[0], "/") {
		return errors.New("worker and verifier require absolute executable paths")
	}
	if s.TimeoutSeconds < 1 || s.TimeoutSeconds > 300 {
		return errors.New("timeout_seconds must be between 1 and 300")
	}
	if len(s.Cases) < 2 || len(s.Cases) > 16 {
		return errors.New("provide 2–16 cases, including replay and holdout")
	}
	ids, splits := map[string]bool{}, map[string]bool{}
	for _, c := range s.Cases {
		if !caseID.MatchString(c.ID) || ids[c.ID] {
			return errors.New("case IDs must be unique safe filenames")
		}
		if c.Split != "replay" && c.Split != "holdout" {
			return errors.New("split must be replay or holdout")
		}
		if c.Input == "" || c.Checks == "" {
			return errors.New("case input and checks directories are required")
		}
		ids[c.ID], splits[c.Split] = true, true
	}
	if !splits["replay"] || !splits["holdout"] {
		return errors.New("both replay and holdout cases are required")
	}
	return nil
}

// Gate derives the decision from paired results; callers never trust Eligible
// from an imported report. This is a local human-owned report, not attestation.
func (r Report) Gate() (bool, string) {
	if r.Version != 1 || !r.ValidKind() || r.CompletedAt == nil || len(r.Cases) != len(r.Suite.Cases) {
		return false, "comparison is incomplete"
	}
	if err := r.Suite.Validate(); err != nil {
		return false, err.Error()
	}
	if r.Suite.WorkerProtocol == ConnectedProtocol {
		expected := ConnectedComparisons(r.Suite)
		for i, c := range r.Cases {
			if c.InputHash != expected[i].InputHash || c.ChecksHash != expected[i].ChecksHash {
				return false, "connected case fingerprints differ"
			}
			for _, o := range []Outcome{c.Baseline, c.Candidate} {
				if o.Runtime == nil || o.Runtime.Provider != r.Suite.Connected.Provider || o.Runtime.RequestedModel != r.Suite.Connected.Model || o.Runtime.RequestedEffort != r.Suite.Connected.Effort {
					return false, "connected runtime configuration differs"
				}
				grade, err := GradeConnected(r.Suite.TextCases[i], ConnectedResult{Artifact: o.Artifact, Runtime: o.Runtime, DurationMS: o.DurationMS}, o.Code, r.Suite.CodeImage)
				if err != nil || grade.Status != o.Status {
					return false, "connected answer check differs"
				}
			}
		}
	}
	improved := map[string]bool{}
	inputs := map[string]bool{}
	for i, c := range r.Cases {
		if r.Suite.WorkerProtocol == "multica_runtime_v1" || r.Suite.WorkerProtocol == ConnectedProtocol {
			a, b := c.Baseline.Runtime, c.Candidate.Runtime
			if a.Validate() != nil || b.Validate() != nil || a.Status != "completed" || b.Status != "completed" || a.Provider != b.Provider || a.RequestedModel != b.RequestedModel || a.RequestedEffort != b.RequestedEffort || a.ExecutableHash != b.ExecutableHash || a.PromptHash != b.PromptHash {
				return false, "runtime comparison is incomplete or configuration changed"
			}
		}
		planned := r.Suite.Cases[i]
		if c.ID != planned.ID || c.Split != planned.Split || len(c.InputHash) != 64 || len(c.ChecksHash) != 64 {
			return false, "comparison does not match frozen cases"
		}
		_, inputErr := hex.DecodeString(c.InputHash)
		_, checksErr := hex.DecodeString(c.ChecksHash)
		if inputErr != nil || checksErr != nil || inputs[c.InputHash] {
			return false, "invalid or duplicate case fingerprints"
		}
		inputs[c.InputHash] = true
		if c.Baseline.Status != "passed" && c.Baseline.Status != "failed" {
			return false, "baseline execution error"
		}
		if c.Candidate.Status != "passed" {
			return false, "candidate failed a check or execution"
		}
		if c.Baseline.Status == "failed" {
			improved[c.Split] = true
		}
	}
	if !improved["replay"] || !improved["holdout"] {
		return false, "require an improvement on both replay and holdout with every candidate check passing"
	}
	return true, "checks passed with improvement on replay and holdout; human review required"
}

type cappedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *cappedBuffer) String() string { return b.buffer.String() }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - b.buffer.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

// container has no host workspace, credentials, socket, network or writable host
// mount. A new filesystem and process namespace separates the grader from worker.
func container(ctx context.Context, suite Suite, mounts map[string]string, argv []string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(suite.TimeoutSeconds)*time.Second)
	defer cancel()
	random, err := os.MkdirTemp("", "multica-eval-container-")
	if err != nil {
		return "", "", -1, err
	}
	defer os.RemoveAll(random)
	name := filepath.Base(random)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", name).Run()
	}()
	args := []string{"run", "--name", name, "--rm", "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user=65534:65534", "--cpus=1", "--memory=512m", "--memory-swap=512m", "--pids-limit=64", "--no-healthcheck", "--log-driver=none", "--tmpfs=/work:rw,nosuid,nodev,size=128m,mode=1777", "--tmpfs=/tmp:rw,nosuid,nodev,size=32m,mode=1777", "--workdir=/work", "--env=HOME=/tmp", "--entrypoint=/bin/sh"}
	for target, source := range mounts {
		if strings.ContainsAny(source, ",\n\r") {
			return "", "", -1, errors.New("unsupported fixture path")
		}
		args = append(args, "--mount", "type=bind,source="+source+",target="+target+",readonly")
	}
	args = append(args, suite.Image, "-c", `cp -R /input/. /work/ || exit 125; exec "$@"`, "multica-eval")
	args = append(args, argv...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out, diagnostic cappedBuffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err = cmd.Run()
	if ctx.Err() != nil {
		return out.String(), diagnostic.String(), -1, ctx.Err()
	}
	if out.overflow || diagnostic.overflow {
		return "", "", -1, errors.New("container output exceeded 64 KiB")
	}
	if err == nil {
		return out.String(), diagnostic.String(), 0, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == 1 {
		return out.String(), diagnostic.String(), 1, nil
	}
	return out.String(), diagnostic.String(), -1, fmt.Errorf("container failed: %w", err)
}
