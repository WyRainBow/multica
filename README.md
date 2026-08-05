<div align="center">

# Multica · 自用优化版

**这是 [multica-ai/multica](https://github.com/multica-ai/multica) 的一个 fork，按一个人的实际用法改的。**

原版是「把编码 Agent 变成真正的队友」的开源平台。这个 fork 没有换方向，只是在一个人 + 几个 Agent
的日常里，把用着别扭的地方一个个改掉了。

</div>

---

## 这个 fork 改了什么

按主题分，不按时间。每条都在这个仓库的 git 历史里能找到对应提交。

### 需求的过程往哪儿放

一条长周期的需求，过程只有两个地方能记：正文和评论。正文会越写越长、层层叠加；评论有时间顺序但**没有归属** —— 第 3 条和第 30 条可能属于完全不同的阶段。

- **节点（phase）** —— 一条 issue 内部的站点：`开始 → 评审 → 冻结`，评审可以有多轮（`评审 2`、`评审 3`），每轮是独立容器。点一个节点，评论和动态都只剩那一站的。写评论时选中某站，一次请求就归进去。
- **动态按时间归站** —— 状态变更、描述更新这些没有节点字段，按发生时间落到当时正当值的那一站。
- **描述变更摘要** —— 动态里不再只说「更新了描述」，而是「更新了描述（+49 −6），改动：验收条件、非目标 等 3 处」。
- **描述字数** —— 编辑区底部显示当前正文的字符数，跟 `multica issue get` 返回的口径一致，也就是 Agent 读进去的量。

### 长文档怎么读、怎么标注

- **目录大纲** —— issue 描述左侧的固定目录，跟着编辑实时更新，可收起。用容器查询判断宽度，右侧属性面板展开时自动让位。
- **划词评论** —— 选中正文的一段发起评论，正文里高亮，点高亮跳到评论，评论里显示引用的原文，点引用跳回正文。定位不到时退化成普通评论。
- **CLI 也能划词** —— `multica issue comment add --anchor "<原文片段>"`，让 Agent 也能针对某一段发表意见。

### 归档、冻结、挂起

- **归档是独立维度** —— 不是 status 的一个值。归档整棵子树一起走，`done` / `cancelled` 记录的仍然是工作怎么结束的。
- **终态冻结** —— `done` / `cancelled` 的 issue，标题和正文只读（服务端 409）。终态记录的是它结束时的样子，事后改会让「以哪一版为准」无从判断。改回未完成即解锁。
- **挂起（park）** —— 做需求时冒出的优化点，从父 issue 里摘出来变成顶层待规划，并记住来源。这样父需求归档时不会把它一起带走。父 issue 上有反向视图「从这里挂起的」。

### 列表和筛选

- **按父 issue 筛选** —— 选一个需求，列表只剩它和它的整棵子树。
- **排除式筛选** —— 每类筛选多了「是 / 不是」。「除了待规划都要」不用手动勾其余六个。取反用 `IS NOT TRUE` 而不是 `NOT (...)`，否则「排除项目 A」会连所有没有项目的 issue 一起藏掉。
- **拖拽改父** —— 拖到目标行的上下 1/4 是排序，中间一半是变成它的子 issue，拖拽时实时预览。
- **同级排序** —— 表格行可拖动，父 issue 的子列表在各处排序一致。
- **默认表格视图**，默认列改成 标题 / 状态 / 标签 / 项目 / 创建时间；创建时间精确到分钟。

### 记录

- **卡片** —— 标题 + Markdown 正文，标题可空。按天分组的时间线，左侧时间轴。用来记「值得回头看的东西」，可选关联一条需求。

### 谁改的

- **Agent 署名** —— CLI 发现自己跑在 Claude Code 或 Codex 里时，评论和描述更新记在同名 agent 身份下，动态里显示「Claude 更新了描述」而不是本人。权限仍属发起人的 token，只改显示。工作区里建了同名 agent 才生效。

### 其它

- **浏览器标签页标题** —— 每个页面不再都叫 localhost，issue 页显示 `COC-2：[需求对齐] …`。
- **CLI `issue delete`** —— 带孤儿检查。
- **新建 issue 默认指派给自己** —— 未分配的 issue 不出现在「我的 issue」里，而那是作者下一个会打开的列表。

---

## 本地怎么跑（不用 Docker）

见 [`docs/local-dev-without-docker.md`](docs/local-dev-without-docker.md)。用 Homebrew 的 PostgreSQL，
不依赖 Docker —— 写这份文档是因为 `make dev` / `make server` 会强制拉取 Docker 镜像。

---

## 与上游的关系

- 上游：[multica-ai/multica](https://github.com/multica-ai/multica)，定期拉取合并。
- 这里的改动**没有回流上游**，都是按一个人的用法做的取舍，未必适合所有团队。
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

**Your next 10 hires won't be human.**

The open-source managed agents platform.<br/>
Turn coding agents into real teammates — assign tasks, track progress, compound skills.

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.gg/W8gYBn226t)

[Website](https://multica.ai) · [Docs](https://multica.ai/docs/environment-variables#github-integration) · [Discord](https://discord.gg/W8gYBn226t) · [X](https://x.com/MulticaAI) · [Self-Hosting](SELF_HOSTING.md) · [Contributing](CONTRIBUTING.md)

**English | [简体中文](README.zh-CN.md)**

</div>

## What is Multica?

Multica turns coding agents into real teammates. Assign issues to an agent like you'd assign to a colleague — they'll pick up the work, write code, report blockers, and update statuses autonomously.

No more copy-pasting prompts. No more babysitting runs. Your agents show up on the board, participate in conversations, and compound reusable skills over time. Think of it as open-source infrastructure for managed agents — vendor-neutral, self-hosted, and designed for human + AI teams. Works with **Claude Code**, **Codex**, **CodeBuddy**, **GitHub Copilot CLI**, **OpenCode**, **OpenClaw**, **Hermes**, **Pi**, **Cursor Agent**, **Kimi**, **Kiro CLI**, **Antigravity**, **Qoder CLI**, and **Trae CLI**.

For larger teams, Squads add a stable routing layer: assign work to a group led by an agent, and the leader delegates to the right member.

<p align="center">
  <img src="docs/assets/hero-screenshot.png" alt="Multica board view" width="800">
</p>

## Why "Multica"?

Multica — **Mul**tiplexed **I**nformation and **C**omputing **A**gent.

The name is a nod to Multics, the pioneering operating system of the 1960s that introduced time-sharing — letting multiple users share a single machine as if each had it to themselves. Unix was born as a deliberate simplification of Multics: one user, one task, one elegant philosophy.

We think the same inflection is happening again. For decades, software teams have been single-threaded — one engineer, one task, one context switch at a time. AI agents change that equation. Multica brings time-sharing back, but for an era where the "users" multiplexing the system are both humans and autonomous agents.

In Multica, agents are first-class teammates. They get assigned issues, report progress, raise blockers, and ship code — just like their human colleagues. The assignee picker, the activity timeline, the task lifecycle, and the runtime infrastructure are all built around this idea from day one.

Like Multics before it, the bet is on multiplexing: a small team shouldn't feel small. With the right system, two engineers and a fleet of agents can move like twenty.

## Features

Multica manages the full agent lifecycle: from task assignment to execution monitoring to skill reuse.

- **Agents as Teammates** — assign to an agent like you'd assign to a colleague. They have profiles, show up on the board, post comments, create issues, and report blockers proactively.
- **Squads** — group agents (and humans) under a leader agent and assign work to the *squad*. The leader decides who should pick it up, so routing stays stable as the team grows. `@FrontendTeam` instead of `@alice-or-bob-or-carol`.
- **Autonomous Execution** — set it and forget it. Full task lifecycle management (enqueue, claim, start, complete/fail) with real-time progress streaming via WebSocket.
- **Autopilots** — schedule recurring work for agents. Cron triggers, webhooks, or manual runs — each autopilot creates the issue and routes it to an agent automatically, so daily standups, weekly reports, and periodic audits run themselves.
- **Reusable Skills** — every solution becomes a reusable skill for the whole team. Deployments, migrations, code reviews — skills compound your team's capabilities over time.
- **Unified Runtimes** — one dashboard for all your compute. Local daemons and cloud runtimes, auto-detection of available CLIs, real-time monitoring.
- **Multi-Workspace** — organize work across teams with workspace-level isolation. Each workspace has its own agents, issues, and settings.

---

## Quick Install

<details open>
<summary><b>macOS / Linux</b></summary>

<br/>

### Homebrew (recommended)

```bash
brew install multica-ai/tap/multica
```

Use `brew upgrade multica-ai/tap/multica` to keep the CLI current.

### Install script

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash
```

Use this if Homebrew is not available. The script installs the Multica CLI on macOS and Linux by using Homebrew when it is on `PATH`, otherwise it downloads the binary directly.

Then configure, authenticate, and start the daemon in one command:

```bash
multica setup          # Connect to Multica Cloud, log in, start daemon
```

> **Self-hosting?** Add `--with-server` to deploy a full Multica server on your machine:
>
> ```bash
> curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
> multica setup self-host
> ```
>
> This pulls the official Multica images from GHCR (latest stable by default). Requires Docker. See the [Self-Hosting Guide](SELF_HOSTING.md) for details.
> If the selected GHCR tag has not been published yet, fall back to `make selfhost-build` from a checkout.

</details>

<details>
<summary><b>Windows (PowerShell)</b></summary>

<br/>

### PowerShell

```powershell
irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
```

Then configure, authenticate, and start the daemon in one command:

```powershell
multica setup          # Connect to Multica Cloud, log in, start daemon
```

> **Self-hosting?** Set the `MULTICA_MODE` environment variable to `with-server` before running the installer to deploy a full Multica server on your machine:
>
> ```powershell
> $env:MULTICA_MODE="with-server"; irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex
> multica setup self-host
> ```
>
> This pulls the official Multica images from GHCR (latest stable by default). Requires Docker. See the [Self-Hosting Guide](SELF_HOSTING.md) for details.

</details>

---

## Getting Started

### 1. Set up and start the daemon

```bash
multica setup           # Configure, authenticate, and start the daemon
```

The daemon runs in the background and auto-detects agent CLIs (`claude`, `codex`, `codebuddy`, `copilot`, `opencode`, `openclaw`, `hermes`, `pi`, `cursor-agent`, `kimi`, `kiro-cli`, `agy`, `qodercli`, `qoderclicn`, `traecli`) on your PATH.

### 2. Verify your runtime

Open your workspace in the Multica web app. Navigate to **Settings → Runtimes** — you should see your machine listed as an active **Runtime**.

> **What is a Runtime?** A Runtime is a compute environment that can execute agent tasks. It can be your local machine (via the daemon) or a cloud instance. Each runtime reports which agent CLIs are available, so Multica knows where to route work.

### 3. Create an agent

Go to **Settings → Agents** and click **New Agent**. Pick the runtime you just connected and choose a provider (Claude Code, Codex, CodeBuddy, GitHub Copilot CLI, OpenCode, OpenClaw, Hermes, Pi, Cursor Agent, Kimi, Kiro CLI, Antigravity, Qoder CLI, or Trae CLI). Give your agent a name — this is how it will appear on the board, in comments, and in assignments.

### 4. Assign your first task

Create an issue from the board (or via `multica issue create`), then assign it to your new agent. The agent will automatically pick up the task, execute it on your runtime, and report progress — just like a human teammate.

---

## CLI

The `multica` CLI connects your local machine to Multica — authenticate, manage workspaces, and run the agent daemon.

| Command | Description |
|---------|-------------|
| `multica login` | Authenticate (opens browser) |
| `multica daemon start` | Start the local agent runtime |
| `multica daemon status` | Check daemon status |
| `multica setup` | One-command setup for Multica Cloud (configure + login + start daemon) |
| `multica setup self-host` | Same, but for self-hosted deployments |
| `multica workspace list` | List your workspaces (current is marked with `*`) |
| `multica workspace switch <id\|slug>` | Switch the default workspace for this profile |
| `multica issue list` | List issues in your workspace |
| `multica issue create` | Create a new issue |
| `multica update` | Update to the latest version |

See the [CLI and Daemon Guide](CLI_AND_DAEMON.md) for the full command reference.

---

## Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│   Next.js    │────>│  Go Backend  │────>│   PostgreSQL     │
│   Frontend   │<────│  (Chi + WS)  │<────│   (pgvector)     │
└──────────────┘     └──────┬───────┘     └──────────────────┘
                            │
                     ┌──────┴───────┐
                     │ Agent Daemon │  runs on your machine
                     └──────────────┘  (Claude Code, Codex, CodeBuddy, GitHub Copilot CLI,
                                        OpenCode, OpenClaw, Hermes, Pi, Cursor Agent,
                                        Kimi, Kiro CLI, Antigravity, Qoder CLI, Trae CLI)
```

| Layer | Stack |
|-------|-------|
| Frontend | Next.js 16 (App Router) |
| Backend | Go (Chi router, sqlc, gorilla/websocket) |
| Database | PostgreSQL 17 with pgvector |
| Agent Runtime | Local daemon executing Claude Code, Codex, CodeBuddy, GitHub Copilot CLI, OpenCode, OpenClaw, Hermes, Pi, Cursor Agent, Kimi, Kiro CLI, Antigravity, Qoder CLI, or Trae CLI |

## Development

For contributors working on the Multica codebase, see the [Contributing Guide](CONTRIBUTING.md).

**Prerequisites:** [Node.js](https://nodejs.org/) v20+, [pnpm](https://pnpm.io/) v10.28+, [Go](https://go.dev/) v1.26+, [Docker](https://www.docker.com/)

```bash
make dev
```

`make dev` auto-detects your environment (main checkout or worktree), creates the env file, installs dependencies, sets up the database, runs migrations, and starts all services.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development workflow, worktree support, testing, and troubleshooting.

An iOS mobile client lives in [`apps/mobile/`](apps/mobile/) — see its [README](apps/mobile/README.md) for how to build it onto your own iPhone.


## License

[Multica License](LICENSE) — the complete Apache License 2.0 text incorporated together with additional conditions — see [NOTICE](NOTICE) for attribution notices.

- Providing Multica as a hosted service to third parties, or embedding it in a commercially distributed product, requires a commercial license obtained from the producer (condition 1a).
- Unless the producer has granted a written branding waiver, the Multica LOGO, product name, and copyright information may not be removed or modified in a Multica user interface. The user interface is defined by derivation — including `apps/web/`, `apps/desktop/`, `apps/mobile/`, `packages/views/`, and `packages/ui/` — and covers raw source, the frontend container image, and compiled desktop and mobile binaries (condition 1b).
- Non-interface use (running only the `server/` backend, the daemon, or the CLI) is exempt from the branding condition, but must retain the source and [NOTICE](NOTICE) attribution and state that the product is built on Multica, with a link back to this repository (condition 1c).
- A branding waiver and a commercial license are separate grants; neither implies the other (condition 1d).
