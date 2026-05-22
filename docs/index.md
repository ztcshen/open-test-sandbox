# Documentation / 文档入口

AgentTestBench keeps the core generic and local-first. Start with the
shortest path for your role.

AgentTestBench 保持核心通用、本地优先。你可以按自己的角色从下面的最短路径进入。

## New Users / 新用户

- [Quick Start](quickstart.md): install dependencies, create a SQL Store,
  register and verify an environment, restore a target Docker stack, run the
  workbench, and verify the checkout. 安装依赖、创建 SQL Store、登记并验收环境、
  恢复目标 Docker 环境、启动工作台并验证仓库。
- [Share Kit](share-kit.md): project tagline, short descriptions, demo script,
  and announcement snippets. 项目 tagline、短介绍、demo 脚本和传播文案。
- [API Case Format](api-case-format.md): write one runnable HTTP case and
  understand the Evidence files it creates. 编写一个可运行的 HTTP 用例，并理解生成的 Evidence。
- [Store Backends](store-backends.md): configure SQLite, PostgreSQL, or MySQL
  Stores for personal and team workflows.
  配置个人与团队工作流使用的 SQLite、PostgreSQL 或 MySQL Store。
- [Backend Capabilities](backend-capabilities.md): complete overview of Store,
  Environment Catalog, Docker restore, discovery, execution, reports, Evidence,
  Control plane APIs, and release guardrails. 完整了解 Store、环境目录、Docker 恢复、
  目标发现、执行、报告、证据、控制平面 API 和发布守卫。

## Environment Operators / 环境维护者

- [Quick Start](quickstart.md#register-and-verify-an-environment): Store-backed
  clean-machine restore flow, repository preflight, Docker Compose/start plan,
  health gates, and verification workflow execution. Store-backed 干净机器恢复、
  仓库预检查、Docker Compose/start 计划、健康门禁和验收工作流执行。
- [CLI and API Contracts](cli-api-contracts.md): Environment Catalog API/CLI
  lifecycle, bootstrap plan shape, restore readiness, topology collection, and
  verified publication gates. 环境目录 API/CLI 生命周期、bootstrap plan、restore
  readiness、拓扑采集和 verified 发布门禁。
- [Release Checklist](release-checklist.md): strict real SkyWalking sign-off and
  optional organization-owned MySQL verification path. 真实 SkyWalking 严格验收和组织自有 MySQL 可选验证路径。

## Template Package / Import-Export Artifacts / 模板包与导入导出资产

- [Template Package Format](profile-format.md): manifest fields, split assets,
  audit, install, import, and verify. 模板包清单字段、拆分资产、审计、安装、导入和验证。
- [Template Package Authoring Guide](profile-authoring.md): practical workflow
  for keeping shareable review/import artifacts outside the core repository.
  如何把可分享、可审阅、可导入的测试资产放在核心仓库之外。

Template packages are optional import/export/review/migration artifacts. Daily
testing should use the active SQLite/PostgreSQL/MySQL SQL Store, Environment Catalog,
CLI/API discovery, and the workbench instead of maintaining a separate package
as the normal operating surface.

模板包是可选的导入、导出、审阅和迁移资产；日常测试应使用 active SQLite/PostgreSQL/MySQL
SQL Store、Environment Catalog、CLI/API 发现和工作台，而不是把单独的文件包作为日常维护入口。

## Agent and CI Integrators / Agent 与 CI 接入方

- [CLI and API Contracts](cli-api-contracts.md): discovery, single-interface
  reports, workflow reports, Environment Catalog restore surfaces,
  asynchronous batch reports, topology collection, and failed-case Evidence
  lookup. 目标发现、单接口报告、工作流报告、环境恢复接口、异步批量报告、拓扑采集和失败证据查询。
- [Release Checklist](release-checklist.md): local and CI gates before sharing
  a public tag. 公开 tag 前的本地和 CI 门禁。

## Project Overview / 项目总览

- [Roadmap](roadmap.md): public development themes and contribution-friendly
  milestones. 公开迭代主题和适合贡献的里程碑。
- [Core Capabilities, Skills, and Goals](core-capabilities-skills-goals.html):
  visual overview of the current workbench capabilities and direction.
  当前工作台能力和方向的可视化总览。
