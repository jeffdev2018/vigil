/**
 * Native path: "Your first run".
 *
 * Written to a new issue, assigned to Mika on the workspace's native
 * runtime, by the welcome hook right after native onboarding bootstraps
 * Mika (OS plan, chantier 5). Unlike the install-runtime guide, this issue
 * is assigned to the AGENT, not the member — creating it starts a real run
 * immediately, so the person lands on a page that is already working.
 *
 * Title is stable so any future dedupe code can match it by title, mirroring
 * `INSTALL_RUNTIME_ISSUE_TITLE`.
 */
export const FIRST_RUN_ISSUE_TITLE = {
  en: "Your first run",
  zh: "你的第一次运行",
  ko: "첫 번째 실행",
  ja: "はじめての実行",
} as const;

const en = `Welcome — this is your first real run in this workspace.

Please:

1. Introduce yourself in a couple of sentences.
2. Summarize what this workspace can already do for the team (agents, runtimes, and integrations configured so far).
3. Propose three concrete issues we could tackle first. List them as a numbered list, each with a one-line reason it's a good starting point.

Keep it short, then end with a question inviting me to pick one to start next.`;

const zh = `欢迎——这是你在这个工作区的第一次真实运行。

请你：

1. 用一两句话自我介绍。
2. 总结一下这个工作区目前已经能为团队做什么（已配置的智能体、运行时和集成）。
3. 提出三个可以优先处理的具体任务，用有序列表列出，每一项附一句话说明为什么值得先做。

保持简短，结尾用一个问题邀请我选一个开始。`;

const ko = `환영합니다 — 이번이 이 workspace에서의 첫 번째 실제 실행입니다.

다음을 해주세요:

1. 한두 문장으로 자기소개를 합니다.
2. 이 workspace가 지금까지 팀을 위해 무엇을 할 수 있는지 요약합니다 (지금까지 설정된 agent, runtime, 연동).
3. 먼저 처리하면 좋을 구체적인 이슈 세 가지를 번호 목록으로 제안하고, 각각 왜 좋은 시작점인지 한 줄로 설명합니다.

간결하게 작성하고, 마지막에 하나를 골라 시작해도 될지 묻는 질문으로 마무리해 주세요.`;

const ja = `ようこそ — これがこの workspace でのはじめての実行です。

次のことをしてください:

1. 一言か二言で自己紹介する。
2. この workspace がチームのためにこれまで何ができるようになったかをまとめる (これまでに設定した agent、runtime、連携)。
3. 最初に取り組むとよさそうな具体的な issue を 3 つ、番号付きリストで提案し、それぞれになぜ良い出発点なのか一言添える。

簡潔にまとめ、最後にどれから始めるか選んでもらう質問で締めくくってください。`;

export const FIRST_RUN_ISSUE_BODY = { en, zh, ko, ja } as const;
