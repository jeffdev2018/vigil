package memoryeval

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestBusinessAnswerChecks(t *testing.T) {
	runtime := &RuntimeEvidence{Provider: "codex", RequestedModel: "fixture", Status: "completed", ExecutableHash: strings.Repeat("a", 64), PromptHash: strings.Repeat("b", 64), BriefHash: strings.Repeat("c", 64)}
	for _, tt := range []struct{ expected, artifact, status string }{
		{`{"queue":"billing","hours":24}`, `{"hours":24.0,"queue":"billing"}`, "passed"},
		{`{"hours":24}`, `{"hours":"24"}`, "failed"},
		{`{"hours":24}`, `{"hours":24,"extra":true}`, "failed"},
		{`9007199254740993`, `9007199254740992`, "failed"},
		{`1e2`, `100.0`, "passed"},
		{`1e99999999999999`, `2e99999999999999`, "failed"},
		{`[1,2]`, `[2,1]`, "failed"},
		{`null`, `null true`, "failed"},
		{`{"a":null}`, `{"b":null}`, "failed"},
	} {
		o, err := GradeConnected(ConnectedCase{Check: "json", Expected: tt.expected}, ConnectedResult{Artifact: tt.artifact, Runtime: runtime}, nil, "")
		if err != nil || o.Status != tt.status {
			t.Fatalf("%s / %s: %+v %v", tt.expected, tt.artifact, o, err)
		}
	}
	c := ConnectedCase{Check: "javascript", Expected: `[{"args":[2],"expected":4}]`}
	image := "sha256:" + strings.Repeat("a", 64)
	result := ConnectedResult{Artifact: "module.exports = x => x*2", Runtime: runtime}
	observation := &CodeObservation{Binding: codeBinding(c, result.Artifact, image), Outputs: []byte(`[{"value":4}]`)}
	o, err := GradeConnected(c, result, observation, image)
	if err != nil || o.Status != "passed" {
		t.Fatalf("%+v %v", o, err)
	}
	result.Artifact += ";"
	if _, err := GradeConnected(c, result, observation, image); err == nil {
		t.Fatal("changed artifact accepted")
	}
	for _, expected := range []string{`[]`, `[{"args":[],"extra":1,"expected":1}]`, `[{"expected":1}]`, `[{"args":[]}]`} {
		if _, err := functionTests(expected); err == nil {
			t.Fatalf("invalid tests accepted: %s", expected)
		}
	}
}

func TestDockerJavaScriptAnswerChecks(t *testing.T) {
	image := os.Getenv("MULTICA_EVAL_NODE_TEST_IMAGE")
	if image == "" {
		t.Skip("explicit local Node image required")
	}
	c := ConnectedCase{Check: "javascript", Expected: `[{"args":[2],"expected":4},{"args":[3],"expected":6}]`}
	for _, tt := range []struct{ code, status string }{
		{`module.exports = x => x*2`, "passed"},
		{`module.exports = x => x*3`, "failed"},
		{`module.exports = () => { throw Error('no'); }`, "failed"},
		{`process.exit(0)`, "error"},
		{`while(true){}`, "error"},
		{`module.exports = x => { const fs=require('fs'); if(fs.readdirSync('/input').sort().join(',')!=='answer.cjs,args.json,runner.cjs') throw Error('unexpected mount'); if(fs.readFileSync('/input/args.json','utf8').includes('expected')) throw Error('answers leaked'); if(Object.keys(require('os').networkInterfaces()).some(k=>k!=='lo')) throw Error('network'); return x*2; }`, "passed"},
	} {
		obs := VerifyJavaScript(context.Background(), c, tt.code, image)
		o, err := gradeAnswerCheck(c, Outcome{Artifact: tt.code}, obs, image)
		if err != nil || o.Status != tt.status {
			t.Fatalf("%s: %+v %v", tt.code, o, err)
		}
	}
}
