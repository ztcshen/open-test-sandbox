# Open Test Sandbox

[![CI](https://github.com/ztcshen/open-test-sandbox/actions/workflows/ci.yml/badge.svg)](https://github.com/ztcshen/open-test-sandbox/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) | **简体中文**

Open Test Sandbox 是一个本地优先、API 驱动的测试沙箱工作台。它帮助测试工程师
和自动化 agent 发现可测目标、执行接口用例或工作流、记录可复现 Evidence，并
生成紧凑的 HTML/JSON 报告，同时保持开源核心通用、可复用。

## 产品方向

当前产品方向是 Store-first：

- 测试工程师通过沙箱 API 和 UI 使用能力，不维护另一个沙箱项目，也不反复编辑外部配置。
- PostgreSQL 是默认活动 Store，保存当前沙箱状态、运行时事实、工作流目录、
  执行状态、Evidence 索引和验证结果。本地 PostgreSQL 与远端 PostgreSQL
  使用同一套日常命令，通过 named Store 配置切换当前活动数据库。
- SQLite 仅保留给旧本地数据导入、兼容检查和测试，不作为新日常工作流的产品路径。
- 可选模板包只用于导入、导出、分享、审阅或迁移，不是日常测试入口。
- 新功能优先提供 Store-first API 和 UI，再考虑文件包兼容桥。

## 为什么需要它

集成测试和回归测试里常见的痛点是：

- 测试资产散落在代码、数据库、脚本和私有文档里；
- 自动化 agent 很难稳定发现目标 ID 和运行上下文；
- 用例失败时，缺少请求、响应、断言、日志、耗时、拓扑等证据，排查很慢。

Open Test Sandbox 把这些能力收束成一个本地控制平面。Store 是活动事实源；
CLI、Control plane API、React 工作台、报告和验证工具读取同一份事实。

## 核心能力

| 能力 | 说明 |
| --- | --- |
| PostgreSQL Store-first | named PostgreSQL Store 保存 schema、运行索引、用例运行、Evidence 索引、耗时、日志、拓扑和后处理任务。 |
| API 驱动目录 | 服务、工作流、接口节点、用例、请求模板、夹具、依赖和绑定通过沙箱 API 与 UI 发现。 |
| Agent 友好的目标发现 | agent 先调用发现 API，再用返回的精确 ID 执行报告。 |
| API 用例执行 | 执行单个 HTTP 用例、已维护用例集合，或只执行集合中失败/未运行的部分；渲染请求、检查断言、写 Evidence，并索引进 Store。 |
| 工作流执行 | 按顺序执行工作流步骤，并保留每一步 Evidence、耗时、状态、日志和拓扑。 |
| 证据详情 API | 按 run 或 case run 查询请求、响应、断言、前置上下文、拓扑、日志、产物清单、失败摘要、状态和耗时。 |
| Control plane 工作台 | React 页面读取同一套 Store/read-model，和 CLI/API 共用运行事实。 |
| 开源守卫 | release-check 防止生成态和来源域词汇进入通用核心。 |

## 适合谁

- **测试工程师**：需要通过稳定 API 和 UI 使用沙箱，而不是维护另一个本地项目。
- **QA 和平台团队**：需要可复现的本地工作台来管理集成用例、工作流回归和运行证据。
- **Agent 构建者**：需要清晰的“先发现、再执行”契约。
- **后端团队**：需要带请求、响应、断言、耗时、日志和拓扑上下文的失败报告。

## 快速开始

安装依赖并验证仓库：

```sh
npm ci
./bin/otsandbox.sh version
OTSANDBOX_DEMO_STORE='postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable' npm run demo:api-case
OTSANDBOX_SMOKE_STORE_DSN='postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable' npm run release-check
```

`demo:api-case` 会启动一个临时本地 HTTP 服务，执行
`examples/api-cases/create-item.json`，写入 active PostgreSQL Store 或
`OTSANDBOX_DEMO_STORE=postgres://...`，并打印 Evidence 目录。`release-check`
要求提供 PostgreSQL smoke Store DSN，会运行空白检查、生成态检查、核心守卫、
Go 测试、demo、React build、active PostgreSQL CLI smoke 和 PostgreSQL-only
无头浏览器冒烟。

## 架构

```text
沙箱 API 和 UI
  -> 活动 PostgreSQL Store
  -> catalog read-model
  -> CLI discovery、Control plane API、React 工作台
  -> 用例与工作流执行
  -> Evidence 文件与 Store 索引
  -> JSON / HTML 报告
  -> 失败用例详情 API
```

核心模块保持通用：

- `cmd/otsandbox/`：CLI 入口和命令编排。
- `internal/store/`：Store 契约和运行记录。
- `internal/store/postgres/`：默认产品 Store 后端。
- `internal/store/sqlite/`：旧兼容与迁移后端。
- `internal/controlplane/`：HTTP API、工作台数据、报告和 Evidence 视图。
- `internal/apicase/`：HTTP 用例 runner 和 Evidence 写入。
- `control-plane/frontend/`：React 工作台源码。
- `control-plane/static/`：`otsandbox serve` 提供的静态工作台资源。

## 文档

| 文档 | 内容 |
| --- | --- |
| [Quick Start](docs/quickstart.md) | 首次本地运行、Store 初始化和工作台启动方向。 |
| [Backend Capabilities](docs/backend-capabilities.md) | Store、目标发现、执行、报告、Evidence、API 和发布守卫。 |
| [Share Kit](docs/share-kit.md) | 项目 tagline、短介绍、demo 脚本和传播文案。 |
| [Roadmap](docs/roadmap.md) | 公开迭代主题和适合贡献的里程碑。 |
| [API Case Format](docs/api-case-format.md) | 可运行 HTTP 用例 JSON 和 Evidence 输出契约。 |
| [CLI and API Contracts](docs/cli-api-contracts.md) | agent/CI 目标发现、报告、异步批量和失败证据查询。 |
| [Release Checklist](docs/release-checklist.md) | 发布前的本地和 CI 门禁。 |
| [Visual Overview](docs/core-capabilities-skills-goals.html) | 双语能力地图、API 面、数据流和迭代目标。 |

## 项目原则

- 默认开发体验必须本地、轻量、可复现，同时日常产品路径使用 named PostgreSQL Store。
- 本地 PostgreSQL 和远端 PostgreSQL 的命令形态必须一致：切换活动 Store，而不是改日常命令。
- 测试工程师应该调用沙箱 API 或使用 UI，不维护单独的配置项目。
- Evidence、报告、日志和本地数据库都是运行产物。
- agent 必须先发现目标，再执行报告。
- CLI、API、Store 或报告契约变化时，要同步更新文档。

## 当前状态

项目仍是 pre-1.0。部分内部包名和兼容命令还保留了早期文件包设计痕迹；
这些是迁移债务，不是目标产品模型。

当前工作区：

- Store 生命周期：status、upgrade、运行索引、契约测试；
- 维护能力：API 用例元数据、可搜索用例目录和集合覆盖；
- 执行能力：单 API 用例、已维护用例集合、接口节点报告、工作流报告；
- Evidence：请求、响应、断言、摘要、日志、拓扑、耗时；
- 工作台：基于 Control plane API 的本地 React 页面；
- 发布门禁：`npm run release-check`。

后续重点是 Store-first 注册 API、更清晰的当前状态工作台、更强的后处理调度、
已验收环境一键拉起，以及更丰富的公开示例。

## 贡献

提交变更前请运行完整本地门禁：

```sh
OTSANDBOX_SMOKE_STORE_DSN='postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable' npm run release-check
```

更多信息见 [CONTRIBUTING.md](CONTRIBUTING.md)、[SECURITY.md](SECURITY.md)
和 [docs/release-checklist.md](docs/release-checklist.md)。Open Test Sandbox
使用 [Apache License 2.0](LICENSE)。
