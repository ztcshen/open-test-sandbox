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
npm run demo:services -- --port 49190
AGENT_TESTBENCH_DEMO_STORE="postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable" npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN="postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable" npm run release-check
# MySQL:
AGENT_TESTBENCH_DEMO_STORE="mysql://user:pass@host:3306/agent_testbench_smoke?tls=false" npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN="mysql://user:pass@host:3306/agent_testbench_smoke?tls=false" npm run release-check
```

What to point out:

- `agent-testbench research features --filter "case"` lists the external
  feature radar index first, so feature search starts from capabilities instead
  of repository names.
- `agent-testbench research feature --feature "case run" --require-min-matches 3`
  reads the external GitHub Feature Radar index before CLI design work, so new
  capabilities can be grounded in active 3K+ star OSS references without
  bundling crawler data into core. Its `nextCommands` field then points the
  demo or implementation back to runnable AgentTestBench CLI surfaces and marks
  whether each recommendation still exists in the current command catalog.
- `agent-testbench research status --max-age-hours 72` checks whether the
  external feature index is fresh enough to trust, reports feature/reference
  and project-index counts, and prints the refresh, audit, coverage, and index
  commands needed when it is stale.
- `agent-testbench research audit --min-references 3` verifies the local radar
  index before use: each reference needs a GitHub name, URL, star floor,
  pushed-after recency, enough peers in its feature, and every maintained
  project-index entry must still be attached to at least one feature.
- `agent-testbench research coverage --min-references 3` checks that every
  indexed feature has enough recent 3K+ star references before roadmap, demo,
  or design work starts.
- `agent-testbench research matrix --filter "workflow" --limit 3` keeps the
  search feature-first, then explains the reference projects with language,
  matched feature ids, and evidence reasons from the maintained project index.
- `agent-testbench research refresh-plan --min-references 3 --max-age-hours 72`
  merges freshness, audit, and coverage checks into the next maintenance plan:
  why the radar needs refresh, which features need more references, and which
  external radar commands to run.
- `agent-testbench research roadmap --min-references 3` ranks the next feature
  candidates by reference coverage, catalog-verified next commands, and star
  signal, then prints a ready-to-run `research plan` command for each item.
- `agent-testbench research backlog --min-references 3` turns the roadmap into
  stateless prioritized tasks with references, implementation commands,
  verification commands, and acceptance criteria.
- `agent-testbench research plan --feature "case run" --require-min-matches 3`
  turns the same feature search into a compact plan: reference gate, ranked
  projects, catalog-verified next commands, and verification commands. Add
  `--format markdown` to produce a reviewable runbook for demos or design notes.
- `/demo-gallery.html` now opens with a CLI automation animation: restore a
  target runtime, rank risky cases, run a case, produce a workflow report,
  process Evidence tasks, identify a Root cause, and publish a quality report.
- `demo:services` starts neutral demo targets for retail fulfillment, IoT
  telemetry control, and content moderation. They are public-facing examples,
  not private business flows.
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
- `docs/demo-gallery.md` and `/demo-gallery.html` provide a visual CLI tour for
  README screenshots, conference proposals, and short social demos.

讲解重点：

- `agent-testbench research features --filter "case"` 会先列出外部 feature radar
  索引，让搜索从能力点开始，而不是从仓库名开始。
- `agent-testbench research feature --feature "case run" --require-min-matches 3`
  会读取外部 GitHub Feature Radar 索引，让新的 CLI 能力先对标近期活跃且
  3K+ star 的开源项目，但核心仓库不内置爬虫数据；返回的 `nextCommands`
  会继续指向可执行的 AgentTestBench CLI 验证入口，并标记推荐命令是否仍存在于当前命令目录。
- `agent-testbench research status --max-age-hours 72` 会先检查外部 feature
  index 是否足够新鲜可信，报告 feature/reference 与项目索引数量；如果过期，
  会打印 refresh、audit、coverage 和 index 的恢复命令。
- `agent-testbench research audit --min-references 3` 会在使用前验证本地 radar
  index：每个引用都需要 GitHub 名称、URL、star 下限、最近更新时间，并且所属 feature
  有足够同类参考；维护中的项目索引也必须仍然挂到至少一个 feature。
- `agent-testbench research coverage --min-references 3` 会检查所有已索引
  feature 是否都有足够的近期 3K+ star 参考，可作为 roadmap、demo 和设计前置门禁。
- `agent-testbench research matrix --filter "workflow" --limit 3` 会保持从
  feature 出发，再用维护好的项目索引解释参考项目的语言、覆盖到的 feature id
  和证据原因。
- `agent-testbench research refresh-plan --min-references 3 --max-age-hours 72`
  会把新鲜度、审计和覆盖检查合成下一次维护计划：为什么需要刷新、哪些 feature
  需要补引用，以及应该执行哪些外部 radar 命令。
- `agent-testbench research roadmap --min-references 3` 会按引用覆盖、命令目录已校验的
  next commands 和 star 信号排序下一批 feature 候选，并为每一项输出可直接执行的
  `research plan` 命令。
- `agent-testbench research backlog --min-references 3` 会把 roadmap 转成无状态
  优先级任务，包含参考项目、实现命令、验证命令和验收条件。
- `agent-testbench research plan --feature "case run" --require-min-matches 3`
  会把同一套 feature 搜索整理成紧凑计划：参考门禁、排序后的项目、经过命令目录校验的
  next commands，以及 verification commands；加 `--format markdown` 可以生成便于评审或演示的 runbook。
- `/demo-gallery.html` 现在包含 CLI 自动化演示动画：恢复目标运行时、排序高风险用例、
  执行用例、生成 workflow report、处理 Evidence tasks、定位 Root cause，并发布质量报告。
- `demo:services` 会启动零售履约、IoT 遥测控制和内容审核三个通用 demo target，
  用于公开展示，不包含私有业务流程。
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
- `docs/demo-gallery.md` 与 `/demo-gallery.html` 提供可截图、可演示的 CLI 可视化导览。

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

1. Open `demo-gallery.html` and let the CLI automation animation play once.
2. Point out `environment restore`, `case suite priority`, `case run`,
   `workflow report`, `evidence tasks`, and `case suite quality-report`.
3. Start `npm run demo:services -- --port 49190`.
4. Run the generic API case demo or one neutral scenario case.
5. Open the generated Evidence bundle and connect it back to the Root cause panel.
6. Show that private team examples live outside the generic core.

1. 打开 `demo-gallery.html`，先完整播放一遍 CLI 自动化动画。
2. 指出 `environment restore`、`case suite priority`、`case run`、
   `workflow report`、`evidence tasks` 和 `case suite quality-report`。
3. 启动 `npm run demo:services -- --port 49190`。
4. 运行通用 API case demo 或一个通用场景 case。
5. 打开生成的 Evidence，并对应到页面里的 Root cause 面板。
6. 展示私有团队示例如何保留在通用核心之外。
