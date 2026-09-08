package sandboxrun

import (
	"reflect"
	"strings"
	"testing"
)

func join(argv []string) string { return strings.Join(argv, " ") }

func TestArgvNonePassesThrough(t *testing.T) {
	t.Parallel()
	argv0, args, env, err := Argv(Spec{Mode: ModeNone}, "/w", []string{"A=1"}, "/usr/bin/claude", []string{"-p", "x"})
	if err != nil || argv0 != "/usr/bin/claude" || !reflect.DeepEqual(args, []string{"-p", "x"}) || !reflect.DeepEqual(env, []string{"A=1"}) {
		t.Fatalf("got %q %q %q %v", argv0, args, env, err)
	}
}

func TestArgvContainer(t *testing.T) {
	t.Parallel()
	env := []string{
		"PATH=/usr/bin", "HOME=/Users/jeff", "SECRET_THING=no",
		"MULTICA_TASK_ID=t1", "MULTICA_SERVER_URL=http://127.0.0.1:8080", "MULTICA_TASK_CONFIG_ROOT=/tmp/task/cfg",
		"ANTHROPIC_API_KEY=sk", "TMPDIR=/tmp/task",
	}
	spec := Spec{Mode: ModeContainer, TaskID: "0123456789abcdef", Provider: "claude", ContainerName: "multica-run-01234567-ab",
		ProxyURL: "http://multica:tok@host.docker.internal:4000", Home: "/Users/jeff/.multica/sandbox/home/claude",
		CLIPath: "/opt/multica", Mounts: []string{"/tmp/task", "/Users/jeff/.codex/home"}}
	argv0, args, _, err := Argv(spec, "/work/dir", env, "/usr/local/bin/claude", []string{"-p", "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if argv0 != "docker" {
		t.Fatalf("argv0 = %q", argv0)
	}
	got := join(args)
	for _, want := range []string{
		"run --rm -i --init --name multica-run-01234567-ab --network multica-sandbox --add-host host.docker.internal:host-gateway",
		"--cap-drop ALL --security-opt no-new-privileges --pids-limit 1024 -w /work/dir -v /work/dir:/work/dir",
		"-v /tmp/task:/tmp/task", "-v /tmp/task/cfg:/tmp/task/cfg", "-v /Users/jeff/.codex/home:/Users/jeff/.codex/home",
		"-v /Users/jeff/.multica/sandbox/home/claude:/root", "-v /opt/multica:/usr/local/bin/multica:ro",
		"-e HOME=/root -e IS_SANDBOX=1",
		"-e HTTPS_PROXY=http://multica:tok@host.docker.internal:4000 -e HTTP_PROXY=http://multica:tok@host.docker.internal:4000 -e NO_PROXY=host.docker.internal,localhost,127.0.0.1",
		"-e MULTICA_TASK_ID=t1", "-e MULTICA_SERVER_URL=http://host.docker.internal:8080", "-e ANTHROPIC_API_KEY=sk", "-e TMPDIR=/tmp/task",
		"node:22-bookworm-slim sh -c command -v claude >/dev/null || npm i -g @anthropic-ai/claude-code >/dev/null; exec 'claude' \"$@\" sh -p hi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, leak := range []string{"SECRET_THING", "HOME=/Users/jeff", "PATH=/usr/bin"} {
		if strings.Contains(got, leak) {
			t.Errorf("leaked %q into container env", leak)
		}
	}
	if strings.Count(got, "-v /tmp/task:/tmp/task") != 1 {
		t.Errorf("TMPDIR mount duplicated:\n%s", got)
	}
}

func TestArgvContainerCustomImageSkipsInstallAndUnknownProviderNeedsOne(t *testing.T) {
	t.Parallel()
	_, args, _, err := Argv(Spec{Mode: ModeContainer, Provider: "codex", Image: "ghcr.io/x/codex:1"}, "/w", nil, "/bin/codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := join(args); strings.Contains(got, "npm i") || !strings.HasSuffix(got, `ghcr.io/x/codex:1 sh -c exec 'codex' "$@" sh`) {
		t.Fatalf("custom image argv: %s", got)
	}
	if _, _, _, err := Argv(Spec{Mode: ModeContainer, Provider: "cursor"}, "/w", nil, "/bin/cursor-agent", nil); err == nil || !strings.Contains(err.Error(), "custom image") {
		t.Fatalf("unsupported provider on the default image must refuse before docker runs, got %v", err)
	}
	if _, _, _, err := Argv(Spec{Mode: ModeContainer, Provider: "cursor", Image: "my/cursor"}, "/w", nil, "/bin/cursor-agent", nil); err != nil {
		t.Fatalf("custom image must be accepted: %v", err)
	}
}

func TestArgvSandbox(t *testing.T) {
	t.Parallel()
	env := []string{"TMPDIR=/tmp/task", "MULTICA_TASK_CONFIG_ROOT=/tmp/task/cfg", "HOME=/home/u"}
	argv0, args, gotEnv, err := Argv(Spec{Mode: ModeSandbox, Mounts: []string{"/home/u/.claude"}}, "/work", env, "/usr/bin/claude", []string{"-p", "x"})
	if err != nil || argv0 != "bwrap" {
		t.Fatalf("argv0 %q err %v", argv0, err)
	}
	want := "--ro-bind / / --bind /work /work --bind /tmp/task /tmp/task --bind /tmp/task/cfg /tmp/task/cfg --bind /home/u/.claude /home/u/.claude --dev /dev --proc /proc --tmpfs /run --unshare-pid --die-with-parent -- /usr/bin/claude -p x"
	if got := join(args); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if !reflect.DeepEqual(gotEnv, env) {
		t.Fatal("sandbox mode must keep the environment")
	}
}

func TestArgvUnknownMode(t *testing.T) {
	t.Parallel()
	if _, _, _, err := Argv(Spec{Mode: "vm"}, "/w", nil, "x", nil); err == nil {
		t.Fatal("expected error")
	}
}
