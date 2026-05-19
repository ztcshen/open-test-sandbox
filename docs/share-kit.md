# Share Kit / 项目传播素材

Use this page when introducing Open Test Sandbox in a README link, internal
newsletter, social post, conference proposal, or demo. Keep wording generic:
team-specific examples belong in external import bundle bundles.

这份页面用于在 README、内部分享、社交媒体、技术演讲或 demo 中介绍 Open Test
Sandbox。文案保持通用，具体团队案例应放在外部 import bundle bundle 中。

## One-Liner / 一句话介绍

**Open Test Sandbox is a local-first, API-operated test sandbox workbench for
agent-friendly integration testing, reproducible Evidence, and compact
HTML/JSON reports.**

**Open Test Sandbox 是一个本地优先、配置驱动的测试沙箱工作台，面向 agent 友好的
集成测试、可复现 Evidence 和紧凑 HTML/JSON 报告。**

## Short Description / 短介绍

Open Test Sandbox lets teams keep test assets in external import bundle bundles,
publish them into a selected PostgreSQL Store, run API cases or workflows, and
inspect failed cases through Evidence-rich reports. Agents can discover
runnable targets before executing reports, so prompts do not need hardcoded ids.

Open Test Sandbox 让团队把测试资产维护在外部 import bundle bundle 中，发布到本地
或团队选择的 PostgreSQL Store 后执行 API 用例或工作流，并通过包含 Evidence 的报告
审阅失败用例。agent 可以先发现可测目标，再执行报告，因此提示词不需要写死 ID。

## Longer Pitch / 较完整介绍

Open Test Sandbox is a generic control plane for local integration testing. It
keeps the open-source core free of team-specific data while still supporting
real workflows through external import bundles. The same Store facts power CLI
commands, Control plane APIs, the React workbench, JSON reports, and HTML
reports. This makes it useful for QA teams, backend teams, platform teams, and
agent builders that need repeatable regression reports with request, response,
assertion, timing, log, and topology context.

Open Test Sandbox 是一套通用的本地集成测试控制平面。它让开源核心不包含团队私有
数据，同时通过外部 import bundle 支持真实工作流。同一份 Store 事实被 CLI、Control
plane API、React 工作台、JSON 报告和 HTML 报告共同使用。它适合需要可复现回归
报告的 QA 团队、后端团队、平台团队和 agent 构建者，报告中可以包含请求、响应、
断言、耗时、日志和拓扑上下文。

## Demo Script / 演示脚本

```sh
git clone https://github.com/ztcshen/open-test-sandbox.git
cd open-test-sandbox
npm ci
OTSANDBOX_DEMO_STORE="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run demo:api-case
OTSANDBOX_SMOKE_STORE_DSN="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run release-check
```

What to point out:

- `demo:api-case` starts a temporary local HTTP service and writes Evidence
  indexes to the active PostgreSQL Store or `OTSANDBOX_DEMO_STORE=postgres://...`.
- `release-check` requires a PostgreSQL smoke Store DSN, then runs guardrails,
  Go tests, the demo, the React build, active PostgreSQL CLI smoke, and
  PostgreSQL-only headless browser smoke tests.
- Live SkyWalking validation is a stricter sign-off mode: set
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS`; otherwise the demo uses deterministic synthetic
  topology wiring for repeatable local smoke.
- import bundles are external by design; the core repository stays generic.

讲解重点：

- `demo:api-case` 会启动临时本地 HTTP 服务，并把 Evidence 索引写入 active
  PostgreSQL Store 或 `OTSANDBOX_DEMO_STORE=postgres://...`。
- `release-check` 要求提供 PostgreSQL smoke Store DSN，然后运行守卫、Go 测试、
  demo、React build、active PostgreSQL CLI smoke 和 PostgreSQL-only 无头浏览器冒烟。
- 真实 SkyWalking 验证是更严格的 sign-off 模式：设置
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`、`OTS_TRACE_GRAPHQL_URL` 和
  `OTS_SMOKE_TRACE_IDS`；否则 demo 使用确定性的 synthetic topology wiring
  做可重复本地冒烟。
- import bundle 默认在核心仓库外维护，核心保持通用。

## Social Snippets / 社交文案

### English

Open Test Sandbox is a local-first test sandbox workbench for API-operated
integration testing. It gives agents a clean discover-then-run workflow and
returns Evidence-rich HTML/JSON reports for API cases and workflows.

### 简体中文

Open Test Sandbox 是一个本地优先、配置驱动的测试沙箱工作台。它让 agent 先发现目标、
再执行报告，并为 API 用例和工作流生成包含 Evidence 的 HTML/JSON 报告。

## Suggested Tags / 推荐标签

`testing`, `test-automation`, `integration-testing`, `local-first`,
`developer-tools`, `agent`, `agents`, `evidence`, `postgresql`, `go`, `react`,
`workflow`, `qa-automation`

## Demo Talking Points / 演示提纲

1. Show that the core repository has no bundled team import bundle.
2. Run the generic API case demo.
3. Open the generated Evidence bundle.
4. Explain the discover-then-report agent flow.
5. Show the backend capabilities document.
6. Explain how a team would keep its own import bundle bundle outside core.

1. 展示核心仓库不内置团队 import bundle。
2. 运行通用 API case demo。
3. 打开生成的 Evidence。
4. 说明 agent 的“先发现、再报告”流程。
5. 展示后端能力文档。
6. 说明团队如何在核心仓库外维护自己的 import bundle bundle。
