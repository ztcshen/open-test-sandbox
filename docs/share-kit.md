# Share Kit / 项目传播素材

Use this page when introducing AgentTestBench in a README link, internal
newsletter, social post, conference proposal, or demo. Keep wording generic:
team-specific examples belong in external template packages.

这份页面用于在 README、内部分享、社交媒体、技术演讲或 demo 中介绍
AgentTestBench。文案保持通用，具体团队案例应放在外部 template package 中。

## One-Liner / 一句话介绍

**AgentTestBench is an agent-native test environment for API workflows,
auditable Evidence, and quality gates.**

**AgentTestBench 是一个面向 Agent 的 API 工作流测试环境，围绕可审计 Evidence
和质量门禁构建。**

## Short Description / 短介绍

AgentTestBench gives agents and test engineers a discover-then-run control
plane for API workflow testing. Teams publish generic test assets into a
selected SQL Store, run API cases or workflows, and inspect failed cases
through Evidence-rich reports. SQLite, PostgreSQL, and MySQL are supported SQL
Store engines, so local and team workflows use the same command shape.

AgentTestBench 给 agent 和测试工程师提供“先发现、再执行”的 API 工作流测试控制平面。
团队把通用测试资产发布到选择的 SQL Store 后执行 API 用例或工作流，并通过包含
Evidence 的报告审阅失败用例。SQLite、PostgreSQL 与 MySQL 都是 SQL Store 引擎，
因此本地和团队工作流保持同一套命令形态。

## Longer Pitch / 较完整介绍

AgentTestBench is a generic control plane for agent-native integration testing.
It keeps the open-source core free of team-specific data while still supporting
real workflows through Store-backed catalogs and optional template packages.
The same Store facts power CLI commands, Control plane APIs, the React
workbench, JSON reports, and HTML reports. This makes it useful for QA teams,
backend teams, platform teams, and agent builders that need repeatable
regression reports with request, response, assertion, timing, log, and topology
context.

AgentTestBench 是一套通用的 agent-native 集成测试控制平面。它让开源核心不包含
团队私有数据，同时通过 Store-backed catalog 和可选 template package 支持真实工作流。
同一份 Store 事实被 CLI、Control plane API、React 工作台、JSON 报告和 HTML 报告共同使用。
它适合需要可复现回归报告的 QA 团队、后端团队、平台团队和 agent 构建者，报告中可以
包含请求、响应、断言、耗时、日志和拓扑上下文。

## Demo Script / 演示脚本

```sh
git clone https://github.com/ztcshen/agent-testbench.git
cd agent-testbench
npm ci
AGENT_TESTBENCH_DEMO_STORE="postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable" npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN="postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable" npm run release-check
# MySQL:
AGENT_TESTBENCH_DEMO_STORE="mysql://user:pass@host:3306/agent_testbench_smoke?tls=false" npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN="mysql://user:pass@host:3306/agent_testbench_smoke?tls=false" npm run release-check
```

What to point out:

- `demo:api-case` starts a temporary local HTTP service and writes Evidence
  indexes to the active SQLite/PostgreSQL/MySQL Store or an explicit
  `AGENT_TESTBENCH_DEMO_STORE=postgres://...` /
  `AGENT_TESTBENCH_DEMO_STORE=mysql://...` /
  `AGENT_TESTBENCH_DEMO_STORE=sqlite://...`. MySQL demo Stores must use dedicated
  sandbox/smoke/test/CI-looking database names, not application schemas.
- `release-check` requires a SQLite, PostgreSQL, or MySQL smoke Store DSN, then runs
  guardrails, Go tests, the demo, the React build, active SQL Store CLI smoke,
  and SQL Store headless browser smoke tests.
- Live SkyWalking validation is a stricter sign-off mode: set
  `AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1`, `AGENT_TESTBENCH_TRACE_GRAPHQL_URL`,
  `AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS`, and `AGENT_TESTBENCH_SMOKE_TRACE_IDS` with mappings for every
  configured workflow step;
  otherwise the demo uses deterministic synthetic topology wiring for
  repeatable local smoke.
- template packages are external by design; the core repository stays generic.

讲解重点：

- `demo:api-case` 会启动临时本地 HTTP 服务，并把 Evidence 索引写入 active
  SQLite/PostgreSQL/MySQL Store 或显式 `AGENT_TESTBENCH_DEMO_STORE=postgres://...` /
  `AGENT_TESTBENCH_DEMO_STORE=mysql://...` /
  `AGENT_TESTBENCH_DEMO_STORE=sqlite://...`。
- `release-check` 要求提供 SQLite、PostgreSQL 或 MySQL smoke Store DSN，然后运行守卫、
  Go 测试、demo、React build、active SQL Store CLI smoke 和 SQL Store 无头浏览器冒烟。
- 真实 SkyWalking 验证是更严格的 sign-off 模式：设置
  `AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1`、`AGENT_TESTBENCH_TRACE_GRAPHQL_URL`、
  `AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS` 和 `AGENT_TESTBENCH_SMOKE_TRACE_IDS`，并覆盖配置工作流的
  每一个必需 step；否则 demo
  使用确定性的 synthetic topology wiring 做可重复本地冒烟。
- template package 默认在核心仓库外维护，核心保持通用。

## Social Snippets / 社交文案

### English

AgentTestBench is an agent-native test environment for API workflows,
auditable Evidence, and quality gates. It gives agents a clean
discover-then-run workflow and returns Evidence-rich HTML/JSON reports.

### 简体中文

AgentTestBench 是一个面向 Agent 的 API 工作流测试环境。它让 agent 先发现目标、
再执行报告，并为 API 用例和工作流生成包含 Evidence 的 HTML/JSON 报告。

## Suggested Tags / 推荐标签

`testing`, `test-automation`, `integration-testing`, `local-first`,
`developer-tools`, `agent`, `agents`, `evidence`, `postgresql`, `go`, `react`,
`workflow`, `qa-automation`

## Demo Talking Points / 演示提纲

1. Show that the core repository has no bundled team template package.
2. Run the generic API case demo.
3. Open the generated Evidence bundle.
4. Explain the discover-then-report agent flow.
5. Show the backend capabilities document.
6. Explain how a team would keep its own template package outside core.

1. 展示核心仓库不内置团队 template package。
2. 运行通用 API case demo。
3. 打开生成的 Evidence。
4. 说明 agent 的“先发现、再报告”流程。
5. 展示后端能力文档。
6. 说明团队如何在核心仓库外维护自己的 template package。
