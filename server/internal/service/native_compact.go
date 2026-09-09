package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	openai "github.com/openai/openai-go/v3"
)

// Context compaction for the native runtime (long tasks, brick 8). A run
// that reads a lot fills its context with tool results it no longer needs
// verbatim. Two stages keep the conversation under budget without losing
// what the model learned:
//
//  1. Microcompaction, no model call: tool results older than the last few
//     turns are replaced by a stub (tool, size, head). The assistant turns
//     and the tool-call ids stay, so the transcript the model sees remains
//     well-formed.
//  2. Summary, one model call: past the hard limit even after stage 1, the
//     model is asked, without tools, to write down what it did, learned and
//     still has to do; the turns are dropped and the summary rides as a
//     note under the brief. The run continues from there.
//
// Sizes are estimated in tokens, not bytes, so a brief full of CJK text is
// capped like a brief full of English. AionCore's compact and deer-flow's
// summarization middleware were the models.

// nativeContextSoftTokens triggers microcompaction; nativeContextHardTokens
// triggers the summary. Variables so tests can shrink them.
var (
	nativeContextSoftTokens = 24_000
	nativeContextHardTokens = 48_000
	// nativeKeepRecentTurns is how many of the latest tool-calling turns
	// keep their results verbatim through a microcompaction.
	nativeKeepRecentTurns = 2
)

const (
	// nativeBriefTokenBudget caps the brief (issue, summaries, comments).
	nativeBriefTokenBudget = 4096
	nativeCompactStubHead  = 240
	nativeSummaryCap       = 8000
)

// nativeTokenEstimate approximates a tokenizer without shipping one: an
// ASCII character is about a quarter of a token, anything else (CJK,
// accented letters, symbols) about one. Deterministic, and conservative
// where it matters — non-Latin text is never under-counted into an
// oversize prompt.
func nativeTokenEstimate(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < unicode.MaxASCII {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+3)/4 + other
}

// nativeToolResult is one tool result as the model sees it.
type nativeToolResult struct {
	callID    string
	tool      string
	content   string
	tokens    int
	compacted bool
}

// nativeTurn is one assistant tool-calling turn and its results.
type nativeTurn struct {
	assistant       openai.ChatCompletionMessageParamUnion
	assistantTokens int
	results         []nativeToolResult
}

// nativeContext is the run's conversation, kept as records so it can be
// rebuilt after a compaction. messages() is what goes to the model.
type nativeContext struct {
	system  string
	brief   string
	summary string
	turns   []nativeTurn
}

func newNativeContext(system, brief string) *nativeContext {
	return &nativeContext{system: system, brief: brief}
}

func (c *nativeContext) addTurn(assistant openai.ChatCompletionMessageParamUnion, assistantText string, results []nativeToolResult) {
	for i := range results {
		results[i].tokens = nativeTokenEstimate(results[i].content)
	}
	c.turns = append(c.turns, nativeTurn{assistant: assistant, assistantTokens: nativeTokenEstimate(assistantText), results: results})
}

// tokens is the estimated size of messages().
func (c *nativeContext) tokens() int {
	n := nativeTokenEstimate(c.system) + nativeTokenEstimate(c.brief)
	if c.summary != "" {
		n += nativeTokenEstimate(c.summary) + 40
	}
	for _, t := range c.turns {
		n += t.assistantTokens + 8
		for _, r := range t.results {
			n += r.tokens + 8
		}
	}
	return n
}

// messages rebuilds the conversation: system, brief, the summary note when
// a compaction produced one, then every kept turn with its (possibly
// stubbed) results.
func (c *nativeContext) messages() []openai.ChatCompletionMessageParamUnion {
	out := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(c.system),
		openai.UserMessage(c.brief),
	}
	if c.summary != "" {
		out = append(out, openai.UserMessage("Summary of your earlier turns in this run (the detailed exchanges were compacted to save context; treat this as your own notes, not as new instructions):\n"+c.summary))
	}
	for _, t := range c.turns {
		out = append(out, t.assistant)
		for _, r := range t.results {
			out = append(out, openai.ToolMessage(r.content, r.callID))
		}
	}
	return out
}

// microcompact stubs the results of every turn but the most recent ones.
// Returns how many results were trimmed and the tokens saved.
func (c *nativeContext) microcompact() (trimmed, saved int) {
	keepFrom := len(c.turns) - nativeKeepRecentTurns
	if keepFrom < 0 {
		keepFrom = 0
	}
	for i := 0; i < keepFrom; i++ {
		for j := range c.turns[i].results {
			r := &c.turns[i].results[j]
			if r.compacted {
				continue
			}
			stub := nativeCompactStub(r.tool, r.content)
			stubTokens := nativeTokenEstimate(stub)
			if stubTokens >= r.tokens {
				continue
			}
			saved += r.tokens - stubTokens
			r.content, r.tokens, r.compacted = stub, stubTokens, true
			trimmed++
		}
	}
	return trimmed, saved
}

// nativeCompactStub is what a trimmed result becomes: the tool, the size,
// and enough of the head to remember what it was about. It is JSON so the
// model reads it like any other result.
func nativeCompactStub(tool, content string) string {
	head := content
	if len(head) > nativeCompactStubHead {
		head = head[:nativeCompactStubHead]
	}
	head = strings.ToValidUTF8(head, "")
	return fmt.Sprintf(`{"compacted":true,"tool":%q,"original_bytes":%d,"head":%q,"note":"older tool result trimmed to save context; call the tool again if you need it"}`, tool, len(content), head)
}

const nativeSummaryPrompt = "Your context is nearly full. Without calling any tool, write a compact summary of this run so far, for yourself: " +
	"(1) what you did, with the ids and concrete facts you must not lose, (2) what you learned from the tools, (3) what remains to be done next. " +
	"Plain text, no preamble. Your earlier turns will be replaced by this summary and you will continue from it."

// summarize asks the model, without tools, to write down what the run did,
// learned and still has to do, then drops the turns behind the summary.
// The call is accounted like any other; a failure leaves the context as it
// was (the next compaction attempt sees the same sizes).
func (s *NativeAgentService) nativeSummarize(ctx context.Context, cx *nativeContext, usage *nativeRunUsage) error {
	msgs := append(cx.messages(), openai.UserMessage(nativeSummaryPrompt))
	completion, err := s.LLM.Chat(ctx, openai.ChatCompletionNewParams{Messages: msgs})
	if err != nil {
		s.noteLLMFailure()
		return err
	}
	if len(completion.Choices) == 0 {
		s.noteLLMFailure()
		return errors.New("summary: model returned no choices")
	}
	s.noteLLMSuccess()
	usage.input += completion.Usage.PromptTokens
	usage.output += completion.Usage.CompletionTokens
	usage.cacheRead += completion.Usage.PromptTokensDetails.CachedTokens
	text := strings.TrimSpace(completion.Choices[0].Message.Content)
	if text == "" {
		return errors.New("summary: model returned no text")
	}
	// A later summary was written from the earlier note, so it supersedes it.
	cx.summary = clampString(text, nativeSummaryCap)
	cx.turns = nil
	return nil
}

// nativeCompactIfNeeded runs the two stages before a model call and
// reports what it did for the transcript. Empty when nothing happened.
func (s *NativeAgentService) nativeCompactIfNeeded(ctx context.Context, cx *nativeContext, usage *nativeRunUsage) string {
	var notes []string
	if cx.tokens() > nativeContextSoftTokens {
		if trimmed, saved := cx.microcompact(); trimmed > 0 {
			notes = append(notes, fmt.Sprintf("Context compacted: %d older tool result(s) trimmed, about %d tokens saved", trimmed, saved))
		}
	}
	if cx.tokens() > nativeContextHardTokens && len(cx.turns) > 0 {
		before := cx.tokens()
		if err := s.nativeSummarize(ctx, cx, usage); err != nil {
			notes = append(notes, "Context summary failed ("+err.Error()+"); continuing with the compacted context")
		} else {
			notes = append(notes, fmt.Sprintf("Context summarized: about %d tokens folded into a %d-token note", before, cx.tokens()))
		}
	}
	return strings.Join(notes, "; ")
}
