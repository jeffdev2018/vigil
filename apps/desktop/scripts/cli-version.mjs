// The server gate (pkg/agent CheckMinCLIVersionFor) accepts bare semver or the
// git-describe shape `vX.Y.Z-N-g<hash>`. When no v* tag is reachable (forks,
// shallow clones) `git describe --always` yields a bare hash, and a daemon
// built from it reports no usable CLI version — quick-create then refuses the
// agent. Synthesize the describe shape from the commit count instead, as
// scripts/dev-env.sh does for `make daemon`.
export function describeVersion(described, count, commit) {
  if (/^v\d+\.\d+\.\d+(-\d+-g[0-9a-fA-F]+)?(-dirty)?$/.test(described)) {
    return described;
  }
  const dirty = described.endsWith("-dirty") ? "-dirty" : "";
  return `v0.0.0-${count || "0"}-g${commit}${dirty}`;
}
