# AgentTestBench

[![CI](https://github.com/ztcshen/agent-testbench/actions/workflows/ci.yml/badge.svg)](https://github.com/ztcshen/agent-testbench/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) | **简体中文**

AgentTestBench 是一个面向 Agent 的 API 工作流测试环境，围绕可审计 Evidence
和质量门禁构建。它帮助测试工程师和自动化 agent 发现可测目标、执行接口用例
或工作流、记录可复现 Evidence，并生成紧凑的 HTML/JSON 报告，同时保持开源
核心通用、可复用。

## 产品方向

当前产品方向是 agent-native 和 Store-first：

- 测试工程师和 agent 通过 API 和 UI 使用能力，不维护另一个沙箱项目，也不反复编辑外部配置。
- SQL Store 是活动 Store，保存当前沙箱状态、运行时事实、工作流目录、
  执行状态、Evidence 索引和验证结果。SQLite、PostgreSQL 与 MySQL 都是产品
  SQL Store 引擎，按使用边界选择。本地和远端 SQL Store 使用同一套日常命令，
  通过 named Store 配置切换当前活动数据库。
- SQLite 适合本地/个人 Store；PostgreSQL 与 MySQL 更适合共享、远端和多用户
  Store。旧本地数据导入仍保留为兼容路径。
- 可选模板包只用于导入、导出、分享、审阅或迁移，不是日常测试入口。
- 新功能优先提供 Store-first API 和 UI，再考虑文件包兼容桥。

## 为什么需要它

集成测试和回归测试里常见的痛点是：

- 测试资产散落在代码、数据库、脚本和私有文档里；
- 自动化 agent 很难稳定发现目标 ID 和运行上下文；
- 用例失败时，缺少请求、响应、断言、日志、耗时、拓扑等证据，排查很慢。

AgentTestBench 把这些能力收束成一个本地控制平面。Store 是活动事实源；
CLI、Control plane API、React 工作台、报告和验证工具读取同一份事实。
agent 得到的是“先发现、再执行”的稳定契约，而不是从提示词或私有文档里猜目标 ID。

## 当前形态

- **Store 引擎**：SQLite、PostgreSQL 与 MySQL 都是产品 SQL Store 引擎，适用于不同运行边界。
- **日常命令**：配置或切换一次 named Store 后，本地和远端 SQL Store 使用同一套
  CLI/API/UI 命令。
- **环境目录**：支持登记环境、查看 bootstrap 计划、从远端服务仓库恢复目标
  Docker 环境、记录验收工作流结果，并只发布 verified 环境。
- **验收证明**：verified 环境需要通过的 workflow run、已索引 Evidence，以及写入
  所选 Store 的真实 SkyWalking 拓扑。
- **发布门禁**：通用 SQLite/PostgreSQL/MySQL `release-check` 已接好；组织自有真实环境
  可选验收可以额外走两阶段 real SkyWalking 门禁，并由操作者提供 secrets 和 trace id。

## 核心能力

| 能力 | 说明 |
| --- | --- |
| SQL Store-first | named SQLite、PostgreSQL 或 MySQL Store 保存 schema、运行索引、用例运行、Evidence 索引、耗时、日志、拓扑和后处理任务。 |
| API 驱动目录 | 服务、工作流、接口节点、用例、请求模板、夹具、依赖和绑定通过 AgentTestBench API 与 UI 发现。 |
| Agent 友好的目标发现 | agent 先调用发现 API，再用返回的精确 ID 执行报告。 |
| API 用例执行 | 执行单个 HTTP 用例、已维护用例集合，或只执行集合中失败/未运行的部分；渲染请求、检查断言、写 Evidence，并索引进 Store。 |
| 工作流执行 | 按顺序执行工作流步骤，并保留每一步 Evidence、耗时、状态、日志和拓扑。 |
| 环境恢复 | Store-backed Environment Catalog 可以规划或执行远端仓库准备、紧凑启动文件生成、Docker Compose pull/build/up、健康检查和绑定的验收工作流。 |
| 证据详情 API | 按 run 或 case run 查询请求、响应、断言、前置上下文、拓扑、日志、产物清单、失败摘要、状态和耗时。 |
| 真实拓扑门禁 | synthetic SkyWalking smoke 只验证 wiring；verified 环境发布和可选真实环境验收必须使用 live SkyWalking endpoint 和覆盖配置工作流所有 step 的 trace id。 |
| Control plane 工作台 | React 页面读取同一套 Store/read-model，和 CLI/API 共用运行事实。 |
| 开源守卫 | release-check 防止生成态和来源域词汇进入通用核心。 |

## 适合谁

- **测试工程师**：需要通过稳定 API 和 UI 使用测试环境，而不是维护另一个本地项目。
- **QA 和平台团队**：需要可复现的本地工作台来管理集成用例、工作流回归和运行证据。
- **Agent 构建者**：需要清晰的“先发现、再执行”契约。
- **后端团队**：需要带请求、响应、断言、耗时、日志和拓扑上下文的失败报告。

## 快速开始

安装依赖并验证仓库：

```sh
npm ci
./bin/agent-testbench.sh version
# SQL Store 示例：
# PostgreSQL：
AGENT_TESTBENCH_DEMO_STORE='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run release-check
# MySQL：
AGENT_TESTBENCH_DEMO_STORE='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run release-check
```

主 CLI 名称是 `agent-testbench`；公开配置和 smoke 测试环境变量统一使用
`AGENT_TESTBENCH_*` namespace。

`demo:api-case` 会启动一个临时本地 HTTP 服务，执行
`examples/api-cases/create-item.json`，写入 active SQL Store 或
`AGENT_TESTBENCH_DEMO_STORE=postgres://...` /
`AGENT_TESTBENCH_DEMO_STORE=mysql://...`，并打印 Evidence 目录。
demo 和发布门禁都要求 MySQL Store 使用看起来属于 sandbox/smoke/test/CI 的专用
库名，不要指向业务 schema。
`release-check` 要求提供 PostgreSQL 或 MySQL smoke Store DSN，会运行空白检查、
生成态检查、核心守卫、Go 测试、demo、React build、active SQL Store CLI smoke
和无头浏览器冒烟。

默认 smoke 使用确定性的 synthetic SkyWalking GraphQL provider，只用于可重复
验证本地 wiring，不作为真实 SkyWalking 发布证据。要验证真实拓扑路径，请额外
设置 `AGENT_TESTBENCH_TRACE_GRAPHQL_URL`、`AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS` 和
`AGENT_TESTBENCH_SMOKE_TRACE_IDS`，让配置工作流 smoke 使用真实 trace id。如果最终验收必须
拒绝 synthetic topology evidence，还要设置 `AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1`；
该模式要求 `AGENT_TESTBENCH_SMOKE_TRACE_IDS` 覆盖配置工作流的每一个必需 step。未配置
SkyWalking endpoint 时，拓扑采集必须
明确显示 unavailable、failed 或 skipped，不能生成假拓扑。

## 架构

```text
AgentTestBench API 和 UI
  -> 活动 SQL Store（PostgreSQL 或 MySQL）
  -> catalog read-model
  -> Environment Catalog 与组件图
  -> 远端服务仓库和目标 Docker runtime
  -> CLI discovery、Control plane API、React 工作台
  -> 用例与工作流执行
  -> Evidence 文件与 Store 索引
  -> JSON / HTML 报告
  -> 失败用例详情 API
```

核心模块保持通用：

- `cmd/agent-testbench/`：CLI 入口和命令编排。
- `internal/server/controlplane/`：HTTP API、工作台数据、报告和 Evidence 视图。
- `internal/runner/`：API 用例、请求模板、JUnit 输出、执行器规划和 Evidence 导入等可运行自动化辅助能力。
- `internal/domain/`：通用 profile、用例集、脱敏和审计领域逻辑。
- `internal/store/`：Store 契约和运行记录。
- `internal/store/postgres/`：PostgreSQL 产品 Store 后端。
- `internal/store/mysql/`：MySQL 产品 Store 后端。
- `internal/store/sqlite/`：SQLite Store 后端，用于本地/个人 Store，并保留旧数据导入兼容能力。
- `control-plane/frontend/`：React 工作台源码。
- `control-plane/static/`：`agent-testbench serve` 提供的静态工作台资源。

## 文档

| 文档 | 内容 |
| --- | --- |
| [Quick Start](docs/quickstart.md) | 首次本地运行、Store 初始化和工作台启动方向。 |
| [Backend Capabilities](docs/backend-capabilities.md) | Store、Environment Catalog、干净机器恢复、目标发现、执行、报告、Evidence、API 和发布守卫。 |
| [Share Kit](docs/share-kit.md) | 项目 tagline、短介绍、demo 脚本和传播文案。 |
| [Roadmap](docs/roadmap.md) | 公开迭代主题和适合贡献的里程碑。 |
| [API Case Format](docs/api-case-format.md) | 可运行 HTTP 用例 JSON 和 Evidence 输出契约。 |
| [Store Backends](docs/store-backends.md) | SQLite/PostgreSQL/MySQL Store 设置、团队 Store 边界和 MySQL 安全保护。 |
| [CLI and API Contracts](docs/cli-api-contracts.md) | agent/CI 目标发现、Environment Catalog 生命周期、报告、异步批量、拓扑采集和失败证据查询。 |
| [Release Checklist](docs/release-checklist.md) | 本地门禁、CI 门禁、真实 SkyWalking 要求和可选真实环境验收。 |
| [Visual Overview](docs/core-capabilities-skills-goals.html) | 双语能力地图、API 面、数据流和迭代目标。 |

## 项目原则

- 默认开发体验必须本地、轻量、可复现，同时日常产品路径使用 named SQL Store。
- 本地和远端 SQL Store 的命令形态必须一致：切换活动 Store，而不是改日常命令。
- 测试工程师应该调用沙箱 API 或使用 UI，不维护单独的配置项目。
- Evidence、报告、日志和本地数据库都是运行产物。
- agent 必须先发现目标，再执行报告。
- CLI、API、Store 或报告契约变化时，要同步更新文档。

## 当前状态

项目仍是 pre-1.0。部分内部包名和兼容命令还保留了早期文件包设计痕迹；
这些是迁移债务，不是目标产品模型。

当前工作区：

- Store 生命周期：named SQLite/PostgreSQL/MySQL 配置、active Store 切换、按后端输出
  DDL、schema status/upgrade 和契约测试；
- 维护能力：API 用例元数据、可搜索用例目录、请求模板、夹具、依赖、工作流绑定
  和集合覆盖；
- 执行能力：单 API 用例、已维护用例集合、异步 batch surface、接口节点报告、
  工作流报告和持久化 workflow run 查询；
- Evidence：请求、响应、断言、摘要、日志、拓扑、耗时、产物清单、失败摘要和
  敏感字段脱敏；
- Environment Catalog：Store-backed 环境登记/发现/查看、bootstrap plan、restore
  诊断、组件图 readiness、远端服务仓库准备、Docker Compose/start 编排、健康门禁、
  验收工作流记录和 verified 发布门禁；
- 工作台：基于 Control plane API 的本地 React 页面，支持 catalog、workflow、
  environment、run、Evidence 和 topology 审阅；
- 发布门禁：`AGENT_TESTBENCH_SMOKE_STORE_DSN=postgres://... npm run release-check` 或
  `AGENT_TESTBENCH_SMOKE_STORE_DSN=mysql://... npm run release-check`；组织自有 MySQL
  Store 可选真实验收先运行 `npm run release-check:mysql-real:preflight`，再运行
  `npm run release-check:mysql-real`，并必须同时提供
  `AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1`、`AGENT_TESTBENCH_TRACE_GRAPHQL_URL`、
  `AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS` 和覆盖配置工作流所有 step 的 `AGENT_TESTBENCH_SMOKE_TRACE_IDS`。

剩余可选真实环境证明主要是运营输入，而不是架构缺口：操作者需要提供真实
MySQL Store DSN、真实 SkyWalking GraphQL endpoint、配置工作流 step 数和该
workflow 的 trace-id 映射，才能运行严格门禁。

## 贡献

提交变更前请运行完整本地门禁：

```sh
AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run release-check
# 或
AGENT_TESTBENCH_SMOKE_STORE_DSN='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run release-check
```

更多信息见 [CONTRIBUTING.md](CONTRIBUTING.md)、[SECURITY.md](SECURITY.md)
和 [docs/release-checklist.md](docs/release-checklist.md)。AgentTestBench
使用 [Apache License 2.0](LICENSE)。
