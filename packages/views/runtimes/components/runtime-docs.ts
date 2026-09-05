function docsLocaleSegment(language?: string): string {
  if (language?.startsWith("zh")) return "/zh";
  if (language?.startsWith("ja")) return "/ja";
  if (language?.startsWith("ko")) return "/ko";
  return "";
}

export function daemonRuntimesDocsHref(language?: string): string {
  return `https://multica.ai/docs${docsLocaleSegment(language)}/daemon-runtimes`;
}

export function customRuntimeDocsHref(language?: string): string {
  const base = daemonRuntimesDocsHref(language);
  if (language?.startsWith("zh")) {
    return `${base}#${encodeURIComponent("自定义运行时配置")}`;
  }
  if (language?.startsWith("ja")) {
    return `${base}#${encodeURIComponent("カスタムランタイムプロファイル")}`;
  }
  if (language?.startsWith("ko")) {
    return `${base}#${encodeURIComponent("사용자-지정-런타임-프로필")}`;
  }
  return `${base}#custom-runtime-profiles`;
}

export function cliAuthDocsHref(language?: string): string {
  return `${daemonRuntimesDocsHref(language)}#cli-authentication`;
}

/**
 * Where a provider documents its own CLI sign-in. The runtime page shows this
 * to every provider Multica cannot sign in for it (see CLI_AUTH_PROVIDERS in
 * @multica/core/runtimes/cli-auth): telling someone "authenticate from the
 * host terminal" is only useful with the page that says how.
 *
 * A provider missing here falls back to our own guide, which is the honest
 * answer when the CLI publishes no authentication page — several of the
 * commands the daemon can detect are internal or unpublished.
 */
const CLI_AUTH_PROVIDER_DOCS: Record<string, string> = {
  claude: "https://code.claude.com/docs/en/cli-reference",
  codex: "https://developers.openai.com/codex/local-config",
  "cursor-agent": "https://cursor.com/docs/cli/reference/authentication",
  copilot:
    "https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/authenticate-copilot-cli",
  opencode: "https://opencode.ai/docs/cli/",
  qwen: "https://qwenlm.github.io/qwen-code-docs/en/users/configuration/auth/",
  kimi: "https://moonshotai.github.io/kimi-cli/en/reference/kimi-command.html",
  "kiro-cli": "https://kiro.dev/docs/getting-started/authentication/",
  codebuddy: "https://www.codebuddy.ai/docs/cli/iam",
  qodercli: "https://docs.qoder.com/cli/authentication",
  qoderclicn: "https://docs.qoder.com/cli/authentication",
  agy: "https://antigravity.google/docs/cli/install/",
  openclaw: "https://docs.openclaw.ai/concepts/oauth",
  omp: "https://github.com/can1357/oh-my-pi/blob/main/docs/providers.md",
  zeroclaw: "https://github.com/zeroclaw-labs/zeroclaw",
  traecli: "https://github.com/bytedance/trae-agent",
  codearts:
    "https://support.huaweicloud.com/intl/en-us/qs-codeartsagent/codeartsagent_qs_0004.html",
};

export function cliAuthProviderDocsHref(
  provider: string,
  language?: string,
): string {
  return CLI_AUTH_PROVIDER_DOCS[provider] ?? cliAuthDocsHref(language);
}
