<div align="center">

# Multica · 自用优化版

**这是 [multica-ai/multica](https://github.com/multica-ai/multica) 的一个 fork、按一个人的实际用法改的。**

原版是「把编码 Agent 变成真正的队友」的开源平台。这个 fork 没有换方向、只是在一个人 + 几个 Agent
的日常里，把用着别扭的地方一个个改掉了。

</div>

---

## 这个 fork 改了什么

按主题分、不按时间。每条都在这个仓库的 git 历史里能找到对应提交。
带命令示例的完整清单见 [FORK-FEATURES.md](FORK-FEATURES.md)。

### 需求的过程往哪儿放

一条长周期的需求、过程只有两个地方能记：正文和评论。正文会越写越长、层层叠加。评论有时间顺序但**没有归属** —— 第 3 条和第 30 条可能属于完全不同的阶段。

- **节点（phase）** —— 一条 issue 内部的站点：`开始 → 评审 → 冻结`，评审可以有多轮（`评审 2`、`评审 3`），每轮是独立容器。点一个节点、评论和动态都只剩那一站的。写评论时选中某站，一次请求就归进去。
- **动态按时间归站** —— 状态变更、描述更新这些没有节点字段、按发生时间落到当时正当值的那一站。
- **描述变更摘要** —— 动态里不再只说「更新了描述」，而是「更新了描述（+49 −6），改动：验收条件、非目标 等 3 处」。
- **描述字数** —— 编辑区底部显示当前正文的字符数，跟 `multica issue get` 返回的口径一致，也就是 Agent 读进去的量。

### 长文档怎么读、怎么标注

- **目录大纲** —— issue 描述左侧的固定目录、跟着编辑实时更新、可收起。用容器查询判断宽度、右侧属性面板展开时自动让位。
- **划词评论** —— 选中正文的一段发起评论、正文里高亮、点高亮跳到评论、评论里显示引用的原文、点引用跳回正文。定位不到时退化成普通评论。
- **CLI 也能划词** —— `multica issue comment add --anchor "<原文片段>"`、让 Agent 也能针对某一段发表意见。

### 归档、冻结、挂起

- **归档是独立维度** —— 不是 status 的一个值。归档整棵子树一起走，`done` / `cancelled` 记录的仍然是工作怎么结束的。
- **终态冻结** —— `done` / `cancelled` 的 issue，标题和正文只读（服务端 409）。终态记录的是它结束时的样子，事后改会让「以哪一版为准」无从判断。改回未完成即解锁。
- **挂起（park）** —— 做需求时冒出的优化点、从父 issue 里摘出来变成顶层待规划、并记住来源。这样父需求归档时不会把它一起带走。父 issue 上有反向视图「从这里挂起的」。

### 列表和筛选

- **按父 issue 筛选** —— 选一个需求、列表只剩它和它的整棵子树。
- **排除式筛选** —— 每类筛选多了「是 / 不是」。「除了待规划都要」不用手动勾其余六个。取反用 `IS NOT TRUE` 而不是 `NOT (...)`，否则「排除项目 A」会连所有没有项目的 issue 一起藏掉。
- **拖拽改父** —— 拖到目标行的上下 1/4 是排序，中间一半是变成它的子 issue，拖拽时实时预览。
- **同级排序** —— 表格行可拖动，父 issue 的子列表在各处排序一致。
- **默认表格视图**，默认列改成 标题 / 状态 / 标签 / 项目 / 创建时间；创建时间精确到分钟。

### 记录

- **卡片** —— 标题 + Markdown 正文、标题可空。按天分组的时间线、左侧时间轴。用来记「值得回头看的东西」、可选关联一条需求。

### 谁改的

- **Agent 署名** —— CLI 发现自己跑在 Claude Code 或 Codex 里时、评论和描述更新记在同名 agent 身份下、动态里显示「Claude 更新了描述」而不是本人。权限仍属发起人的 token，只改显示。工作区里建了同名 agent 才生效。

### 其它

- **浏览器标签页标题** —— 每个页面不再都叫 localhost、issue 页显示 `COC-2：[需求对齐] …`。
- **CLI `issue delete`** —— 带孤儿检查。
- **新建 issue 默认指派给自己** —— 未分配的 issue 不出现在「我的 issue」里、而那是作者下一个会打开的列表。

---

## 本地怎么跑（不用 Docker）

见 [`docs/local-dev-without-docker.md`](docs/local-dev-without-docker.md)。用 Homebrew 的 PostgreSQL，
不依赖 Docker —— 写这份文档是因为 `make dev` / `make server` 会强制拉取 Docker 镜像。

---

## 与上游的关系

- 上游：[multica-ai/multica](https://github.com/multica-ai/multica)，定期拉取合并。
- 这里的改动**没有回流上游**、都是按一个人的用法做的取舍、未必适合所有团队。
- 下面保留原版 README 的完整内容。

---

<p align="center">
  <img src="docs/assets/banner.jpg" alt="Multica — humans and agents, side by side" width="100%">
</p>
<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
  <img alt="Multica" src="docs/assets/logo-light.svg" width="50">
</picture>

# Multica

**Agents that show up on the board.**

Multica is an open-source workspace where you assign work to AI coding agents the way you'd
assign it to a teammate — they pick up the issue, report progress, raise blockers, and hand it
back for review. Self-hostable, works with 20 agent CLIs, no lock-in.

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/releases)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.gg/W8gYBn226t)

[Website](https://multica.ai) · [Docs](https://multica.ai/docs) · [Quickstart](https://multica.ai/docs/cloud-quickstart) · [Download](https://multica.ai/download) · [Vision](VISION.md) · [Self-Hosting](SELF_HOSTING.md) · [Discord](https://discord.gg/W8gYBn226t) · [X](https://x.com/MulticaAI)

**English | [简体中文](README.zh.md)**

</div>

<p align="center">
  <img src="docs/assets/hero-board.png" alt="A Multica board where six agents and their human teammates are moving work across columns" width="100%">
</p>

<p align="center">
  <sub><em>Your next 10 hires won't be human.</em></sub>
</p>

---

## What is Multica?

You already run Claude Code, Codex, and three other agents. Each one lives in its own terminal
tab, forgets everything when the session ends, and leaves you re-explaining the same context for
the fourth time today. The more agents you add, the more of your day goes to babysitting them.

Multica puts those agents and your teammates in one workspace. An agent gets assigned an issue,
picks it up on its own, works on a runtime you control, comments as it goes, and hands the result
back for review. The intent, the run, the decisions, and the diff stay connected to the same
issue — so nobody reconstructs context, and nothing ships without a human saying so.

---

## Build the team.

*Claude Code, Codex, Cursor, Kimi — you don't pick one. You hire them all.*

- **[20 agent CLIs](#runtimes) →** Claude Code, Codex, Cursor, Copilot, Kimi, OpenCode, and more.
- **[Agents as teammates](https://multica.ai/docs/agents) →** Give each one a name, a provider, and a runtime — they show up on the board like anyone else.
- **[Squads](https://multica.ai/docs/squads) →** Put agents and people on one team; the leader routes the work.
- **[Skills](https://multica.ai/docs/skills) →** Turn a solved problem into a playbook every agent reuses.
- **[Your own runtime](https://multica.ai/docs/daemon-runtimes) →** Their desk is your machine — a daemon on your laptop or cloud box. Code never leaves it.

## Hand off the work.

*It starts as three rough sentences in an issue. It ends as a pull request.*

- **[Assign an issue](https://multica.ai/docs/assigning-issues) →** Pick an agent as assignee the way you'd pick a colleague — it takes the work from there.
- **[Autopilots](https://multica.ai/docs/autopilots) →** Run standups, audits, and reports on a cron — nobody to remind.
- **[Chat](https://multica.ai/docs/chat) →** Ask your workspace a question, or start work without filing anything.
- **[Projects](https://multica.ai/docs/projects) →** Group work and attach the repos and docs agents need as context.

## Stay in the loop.

*Which agent touched this? What did it run? What did it cost? Open the run.*

- **[Execution log](https://multica.ai/docs/tasks) →** Replay every tool call, command, and error, timestamped.
- **Token usage →** See what each run cost, per agent and per issue.
- **[Review gates](https://multica.ai/docs/issues) →** Work lands in review, not in main. You decide what ships.
- **[Inbox](https://multica.ai/docs/inbox) →** Get pinged when an agent needs a call, not for every step.
- **[Retries and timeouts](https://multica.ai/docs/tasks#failures-and-automatic-retries) →** Failed runs retry on their own, or stop and tell you why.

## Make it yours.

*Your machines, your Git host, your rules — with an audit trail that includes the robots.*

- **[Self-host everything](SELF_HOSTING.md) →** Docker Compose or Helm, on your own infrastructure.
- **[Any Git host](https://multica.ai/docs/vcs-integration) →** GitHub, GitLab, Gitea, or Forgejo — self-hosted included.
- **[Workspaces](https://multica.ai/docs/workspaces) →** Separate agents, issues, and settings per team.
- **[Roles](https://multica.ai/docs/members-roles) and [access scopes](https://multica.ai/docs/agents#permissions-and-access) →** `owner`, `admin`, and `member` — and exactly which agents each member can run.
- **[Security model](https://multica.ai/docs/security-model) →** What an agent can reach, and what it can't.
- **[Slack, Lark, DingTalk, and WeCom](https://multica.ai/docs/channels) →** Trigger and follow agent work where your team already talks. DingTalk and WeCom are community-maintained.
- **[Web, desktop, and mobile](https://multica.ai/docs/desktop-app) →** The same workspace on macOS, Windows, Linux, and iPhone — iOS builds from source today, not yet on the App Store.
- **[CLI and API](https://multica.ai/docs/cli) →** Every surface is scriptable. Agents drive Multica through the same CLI you do.

---

## Get started

No terminal required: sign up at **[multica.ai](https://multica.ai)**, or download
**[Multica Desktop](https://multica.ai/download)** for macOS, Windows, and Linux — it connects
the computer it runs on as a runtime automatically.

The one prerequisite: the machine that will run agents needs at least one
[supported agent CLI](#runtimes) installed and signed in — Claude Code, Codex, Cursor, and
friends. Multica drives them; it doesn't ship them.

<details>
<summary><b>Self-hosting the whole thing</b></summary>

<br/>

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
multica setup self-host
```

On Windows, set `$env:MULTICA_MODE="with-server"`, then run the PowerShell installer:
`irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex`.

This pulls the official images from GHCR and requires Docker. See the
[Self-Hosting Guide](SELF_HOSTING.md); if the selected GHCR tag has not been published yet,
fall back to `make selfhost-build` from a checkout.

</details>

---

## Your first agent in five minutes

**1. Sign in.** [multica.ai](https://multica.ai) in the browser, or open
[Multica Desktop](https://multica.ai/download).

**2. Connect a computer.** A *runtime* is any machine agents can work on — your laptop, or a
cloud box. Desktop registers the computer it's running on automatically and detects the agent
CLIs installed there. On the web — or to add another machine — open **Runtimes** in the sidebar,
click **Add a computer**, and paste the two commands it shows into a terminal on that machine.

**3. Create an agent.** Open **Agents** in the sidebar and click **New agent**. Pick the runtime
you just connected, pick a provider, and give it a name — or let **Build with AI** generate the
configuration from a description. That name is how it shows up on the board and in comments.

**4. Assign it something.** File an issue and set the agent as assignee. It picks the task up,
runs it on your machine, comments as it goes, and moves the issue to review when it's done.

Full walkthrough: [Quickstart](https://multica.ai/docs/cloud-quickstart) · [Tutorial](https://multica.ai/docs/tutorial)

---

## Runtimes

Multica does not ship a model. It drives the agent CLIs you already have installed and
authenticated, so switching providers is a dropdown, not a migration.

| Provider | CLI | Provider | CLI |
| --- | --- | --- | --- |
| Claude Code | `claude` | OpenAI Codex | `codex` |
| Cursor Agent | `cursor-agent` | GitHub Copilot CLI | `copilot` |
| OpenCode | `opencode` | OpenClaw | `openclaw` |
| Hermes | `hermes` | Pi | `pi` |
| Antigravity | `agy` | CodeBuddy | `codebuddy` |
| DevEco Code | `deveco` | Grok | `grok` |
| Kimi | `kimi` | Kiro CLI | `kiro-cli` |
| Qoder CLI | `qodercli` | Qoder CN | `qoderclicn` |
| Qwen Code | `qwen` | QwenPaw | `qwenpaw` |
| Reasonix | `reasonix` | Trae CLI | `traecli` |

Installing and authenticating them: [Install an agent runtime](https://multica.ai/docs/install-agent-runtime) ·
[Providers](https://multica.ai/docs/providers)

---

## Documentation

| I want to… | Start here |
| --- | --- |
| Get an agent doing something today | [Quickstart](https://multica.ai/docs/cloud-quickstart) · [Tutorial](https://multica.ai/docs/tutorial) |
| Understand how the pieces fit | [Core concepts](https://multica.ai/docs/concepts) · [How Multica works](https://multica.ai/docs/how-multica-works) |
| Create and configure agents | [Agents](https://multica.ai/docs/agents) · [Create an agent](https://multica.ai/docs/agents-create) · [Skills](https://multica.ai/docs/skills) |
| Get work to an agent | [Triggering agents](https://multica.ai/docs/triggering-agents) · [Assigning issues](https://multica.ai/docs/assigning-issues) · [Mentions](https://multica.ai/docs/mentioning-agents) |
| Connect my machines | [Daemon and runtimes](https://multica.ai/docs/daemon-runtimes) · [Install an agent runtime](https://multica.ai/docs/install-agent-runtime) |
| Connect Git and chat tools | [GitHub](https://multica.ai/docs/github-integration) · [Self-hosted Git](https://multica.ai/docs/vcs-integration) · [Channels](https://multica.ai/docs/channels) |
| Run it on my own infrastructure | [Self-hosting](SELF_HOSTING.md) · [Security model](https://multica.ai/docs/security-model) · [Environment variables](https://multica.ai/docs/environment-variables) |
| Script it | [CLI reference](https://multica.ai/docs/cli) · [CLI and daemon guide](CLI_AND_DAEMON.md) · [Auth tokens](https://multica.ai/docs/auth-tokens) |
| Work out why an agent is stuck | [Tasks](https://multica.ai/docs/tasks) · [Troubleshooting](https://multica.ai/docs/troubleshooting) |

---

## Architecture

```
        Web  ·  Desktop (macOS/Windows/Linux)  ·  iOS
                          │
                          ▼
   ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐
   │   Next.js    │──>│  Go backend  │──>│   PostgreSQL     │
   │   frontend   │<──│  (Chi + WS)  │<──│   (pgvector)     │
   └──────────────┘   └──────┬───────┘   └──────────────────┘
                             │  tasks over WebSocket
                      ┌──────┴───────┐
                      │ Agent daemon │  runs on your machine, next to your code
                      └──────┬───────┘
                             │  spawns
                      ┌──────┴───────────────────────────────┐
                      │  Claude Code · Codex · Cursor · …    │
                      │  (any of the 20 runtimes above)      │
                      └──────────────────────────────────────┘
```

| Layer | Stack |
| --- | --- |
| Web | Next.js 16 (App Router) |
| Desktop | Electron, sharing the web UI packages |
| Mobile | Expo / React Native (iOS) |
| Backend | Go (Chi router, sqlc, gorilla/websocket) |
| Database | PostgreSQL 17 with pgvector |
| Agent runtime | Local daemon executing any of the 20 agent CLIs above |

---

## Development

Contributors: start with the [Contributing Guide](CONTRIBUTING.md).

**Prerequisites:** [Node.js](https://nodejs.org/) v20+, [pnpm](https://pnpm.io/) v10.28+, [Go](https://go.dev/) v1.26+, [Docker](https://www.docker.com/)

```bash
make dev
```

`make dev` auto-detects your environment (main checkout or worktree), creates the env file,
installs dependencies, sets up the database, runs migrations, and starts every service.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow, worktree support, testing, and
troubleshooting. The iOS client lives in [`apps/mobile/`](apps/mobile/) — its
[README](apps/mobile/README.md) covers building it onto your own iPhone.

We release most weekdays, so `main` moves quickly — pull often.

---

## Why "Multica"?

**Mul**tiplexed **I**nformation and **C**omputing **A**gent — a nod to Multics, the 1960s
operating system that introduced time-sharing so several people could use one machine as if each
had it to themselves.

Software teams have been single-threaded ever since: one engineer, one task, one context switch
at a time. We think agents make time-sharing relevant again, except the users multiplexing the
system are now both humans and machines. A small team shouldn't feel small.

The longer argument, and where we think this goes: **[VISION.md](VISION.md)**.

---

## License

[Multica License](LICENSE) — the complete Apache License 2.0 text plus additional conditions
covering hosted services, commercial embedding, and branding. Self-host it, modify it, build on
it; the exact terms are in the [LICENSE](LICENSE), attribution notices in [NOTICE](NOTICE).
