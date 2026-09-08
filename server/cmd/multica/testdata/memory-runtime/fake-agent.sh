#!/bin/sh
# Deterministic protocol fixture. This is not an LLM or a quality benchmark.
set -eu
read -r prompt
printf '%s' "$prompt" > received-prompt.json
printf '%s\n' "$@" > received-args.txt
answer=baseline
if grep -q 'Use independent assertions' CLAUDE.md; then answer=fixed; fi
printf '%s\n' '{"type":"assistant","message":{"model":"observed-fixture","content":[{"type":"tool_use","id":"read-1","name":"Read","input":{}}]}}'
printf '{"type":"result","subtype":"success","is_error":false,"result":"%s","modelUsage":{"observed-fixture":{"inputTokens":11,"outputTokens":7,"cacheReadInputTokens":3,"cacheCreationInputTokens":2}}}\n' "$answer"
