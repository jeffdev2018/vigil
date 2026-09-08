package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/memoryeval"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// MemoryRuntimeFixture supplies frozen, non-secret task context. The offline
// command runs it inside Docker; connected comparisons use runtime-owned auth
// and local permissions. Neither path resumes a previous session.
type MemoryRuntimeFixture struct {
	Provider           string `json:"provider"`
	Executable         string `json:"executable"`
	Model              string `json:"model"`
	ThinkingLevel      string `json:"thinking_level"`
	Prompt             string `json:"prompt"`
	AgentName          string `json:"agent_name"`
	AgentInstructions  string `json:"agent_instructions"`
	WorkspaceContext   string `json:"workspace_context"`
	ProjectTitle       string `json:"project_title"`
	ProjectDescription string `json:"project_description"`
	MaxTurns           int    `json:"max_turns"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
}

func RunMemoryRuntime(ctx context.Context, workDir string, fixture MemoryRuntimeFixture, memories []string) (memoryeval.RuntimeOutput, error) {
	var output memoryeval.RuntimeOutput
	// Only the protocol families exercised by connected comparisons are supported.
	if (fixture.Provider != "claude" && fixture.Provider != "codex") || !filepath.IsAbs(fixture.Executable) || strings.TrimSpace(fixture.Model) == "" || strings.TrimSpace(fixture.Prompt) == "" || fixture.MaxTurns < 1 || fixture.MaxTurns > 20 || fixture.TimeoutSeconds < 1 || fixture.TimeoutSeconds > 300 {
		return output, errors.New("runtime fixture requires claude or codex, an absolute executable, model, prompt, 1–20 turns and 1–300 seconds")
	}
	if len(memories) > 200 {
		return output, errors.New("too many memories")
	}
	for _, memory := range memories {
		if len([]rune(memory)) > 500 {
			return output, errors.New("memory exceeds 500 characters")
		}
	}
	exe, err := os.Open(fixture.Executable)
	if err != nil {
		return output, err
	}
	h := sha256.New()
	_, err = io.Copy(h, exe)
	exe.Close()
	if err != nil {
		return output, err
	}
	const runID = "offline-evaluation"
	brief, err := execenv.InjectRuntimeConfig(workDir, fixture.Provider, execenv.TaskContextForEnv{
		AgentName: fixture.AgentName, AgentInstructions: fixture.AgentInstructions, AgentMemories: memories,
		WorkspaceContext: fixture.WorkspaceContext, ProjectID: "offline-project", ProjectTitle: fixture.ProjectTitle, ProjectDescription: fixture.ProjectDescription,
		AutopilotRunID: runID,
	})
	if err != nil {
		return output, err
	}
	prompt := BuildPrompt(Task{AutopilotRunID: runID, AutopilotDescription: fixture.Prompt}, fixture.Provider)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := agent.Config{ExecutablePath: fixture.Executable, Logger: logger}
	if fixture.Provider == "codex" {
		home := filepath.Join(workDir, ".codex-runtime")
		if err := execenv.PrepareCodexRuntimeHome(home, logger); err != nil {
			return output, err
		}
		config.Env = map[string]string{"CODEX_HOME": home}
	}
	backend, err := agent.New(fixture.Provider, config)
	if err != nil {
		return output, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(fixture.TimeoutSeconds)*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, prompt, agent.ExecOptions{Cwd: workDir, Model: fixture.Model, ThinkingLevel: fixture.ThinkingLevel, MaxTurns: fixture.MaxTurns, Timeout: time.Duration(fixture.TimeoutSeconds) * time.Second, McpConfig: json.RawMessage(`{"mcpServers":{}}`)})
	if err != nil {
		return output, err
	}
	observed := &memoryeval.RuntimeEvidence{Provider: fixture.Provider, RequestedModel: fixture.Model, RequestedEffort: fixture.ThinkingLevel, ExecutableHash: fmt.Sprintf("%x", h.Sum(nil)), PromptHash: fmt.Sprintf("%x", sha256.Sum256([]byte(prompt))), BriefHash: fmt.Sprintf("%x", sha256.Sum256([]byte(brief)))}
	var result *agent.Result
	messages, results := session.Messages, session.Result
	for messages != nil || results != nil {
		select {
		case <-ctx.Done():
			return output, ctx.Err()
		case message, ok := <-messages:
			if !ok {
				messages = nil
				continue
			}
			if message.Type == agent.MessageToolUse {
				observed.ToolCalls++
			}
		case value, ok := <-results:
			if !ok {
				results = nil
				continue
			}
			result = &value
		}
	}
	if result == nil {
		return output, errors.New("runtime ended without a result")
	}
	observed.Status = result.Status
	if result.Usage != nil {
		observed.Usage = map[string]memoryeval.RuntimeUsage{}
		for model, u := range result.Usage {
			observed.Usage[model] = memoryeval.RuntimeUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens}
		}
	}
	if err := observed.Validate(); err != nil {
		return output, err
	}
	return memoryeval.RuntimeOutput{Artifact: result.Output, Runtime: observed}, nil
}

// handleMemoryEvaluation holds one slot for the whole paired comparison. A
// claimed request is never executed again after a crash or a lost response.
// ponytail: one evaluation per daemon; use per-runtime slots if throughput requires it.
func (d *Daemon) handleMemoryEvaluation(ctx context.Context, rt Runtime, id string) {
	if !d.memoryEvaluationActive.TryLock() {
		return
	}
	defer d.memoryEvaluationActive.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 18*time.Minute)
	defer cancel()
	var job memoryeval.ConnectedJob
	base := fmt.Sprintf("/api/daemon/runtimes/%s/memory-evaluations/%s", rt.ID, id)
	if err := d.client.postJSON(ctx, base+"/claim", struct{}{}, &job); err != nil {
		return
	}
	report := func(result memoryeval.ConnectedResult) bool {
		var response struct {
			Status string `json:"status"`
		}
		send, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return d.client.postJSON(send, base+"/report", result, &response) == nil && response.Status == "running"
	}
	path, prefix, err := d.resolveRuntimeCommand(ctx, rt)
	if err != nil || len(prefix) != 0 || job.Config.Provider != rt.Provider || len(job.Cases) < 2 || len(job.Cases) > 8 {
		report(memoryeval.ConnectedResult{Failed: true})
		return
	}
	for i, c := range job.Cases {
		for variant := 0; variant < 2; variant++ {
			start := time.Now()
			result := memoryeval.ConnectedResult{Index: i*2 + variant}
			work, err := os.MkdirTemp("", "multica-memory-connected-")
			if err != nil {
				result.Failed = true
				report(result)
				return
			}
			memories := append([]string{}, job.Baseline...)
			if variant == 1 {
				memories = append(memories, job.Candidate)
			}
			f := MemoryRuntimeFixture{Provider: rt.Provider, Executable: path, Model: job.Config.Model, ThinkingLevel: job.Config.Effort, Prompt: c.Prompt, AgentName: job.Config.AgentName, AgentInstructions: job.Config.Instructions, WorkspaceContext: job.Config.WorkspaceContext, MaxTurns: 1, TimeoutSeconds: 60}
			output, err := RunMemoryRuntime(ctx, work, f, memories)
			os.RemoveAll(work)
			result.DurationMS = time.Since(start).Milliseconds()
			if err != nil {
				result.Failed = true
			} else {
				result.Artifact = output.Artifact
				result.Runtime = output.Runtime
			}
			if !report(result) {
				return
			}
		}
	}
}
