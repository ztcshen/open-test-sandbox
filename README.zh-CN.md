# Open Test Sandbox

[![CI](https://github.com/ztcshen/open-test-sandbox/actions/workflows/ci.yml/badge.svg)](https://github.com/ztcshen/open-test-sandbox/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

[English](README.md) | **简体中文**

Open Test Sandbox 是一个本地优先、配置驱动的测试沙箱工作台。它帮助团队和测试
agent 发现可测目标、执行接口用例或工作流、记录可复现 Evidence，并生成紧凑的
HTML/JSON 报告，同时保持核心仓库通用、可开源、可跨团队复用。

Open Test Sandbox is a local-first test sandbox workbench for profile-driven
integration testing. It helps teams and agents discover runnable targets, run
API cases or workflows, record reproducible Evidence, and return compact
HTML/JSON reports without hardcoding one business domain into the core.

## 为什么需要它

集成测试和回归测试里常见的痛点是：

- 测试资产散落在代码、数据库、脚本和私有文档里；
- agent 想自动回归时，不知道应该先查哪些目标，也不知道如何拿到精确 ID；
- 用例失败时，缺少请求、响应、断言、日志、耗时、拓扑等证据，排查很慢。

Open Test Sandbox 把这些能力收束成一个本地控制平面。团队配置放在核心仓库外，
以 profile bundle 的形式维护；核心负责审计、发布、执行、记录 Evidence，并把
同一份事实提供给 CLI、Control plane API、React 工作台和报告模板。

## 核心能力

| 能力 | 说明 |
| --- | --- |
| 本地优先 Store | 默认 SQLite，保存 schema、配置索引、运行记录、用例运行、Evidence 索引、耗时、日志、拓扑和后处理任务。 |
| 外部配置包 | 服务、工作流、接口节点、用例、请求模板、夹具、依赖和绑定都放在核心仓库外。 |
| Agent 友好的目标发现 | agent 先调用 `interface-node discover`、`workflow discover` 或 `case discover`，再用返回的精确 ID 执行报告。 |
| 用例维护目录 | API 用例可维护说明、标签、优先级、owner、状态、可运行文件、执行配置、就绪问题、最新运行状态和可执行计划，便于审阅、分派和集合执行。 |
| API 用例执行 | 执行单个 HTTP 用例、已维护用例集合，或只执行集合中失败/未运行的部分；渲染请求、检查断言、写 Evidence，并可把结果索引进 Store。 |
| 集合覆盖审计 | 不重跑请求，通过 CLI 或 Control plane API 检查已维护用例集合在 Store 中的通过、失败和未运行状态。 |
| 接口与工作流报告 | 执行接口节点下的所有用例，或按顺序执行工作流步骤，输出 JSON 和临时 HTML 报告；已维护集合和批量运行也提供面向 CI/agent 排查的 JUnit XML 和失败摘要。 |
| 证据详情 API | 按 run 或 case run 查询请求、响应、断言、前置上下文、拓扑、日志、产物清单、失败摘要、状态和耗时。 |
| Control plane 工作台 | React 页面读取同一套 Store/read-model，和 CLI/API 共用运行事实。 |
| 开源守卫 | release-check 防止生成态和来源域词汇进入通用核心。 |

## 适合谁

- **QA 和平台团队**：需要可复现的本地工作台来管理集成用例、工作流回归和运行证据。
- **Agent 构建者**：需要清晰的“先发现、再执行”契约，而不是在提示词里写死目标 ID。
- **后端团队**：需要带请求、响应、断言、耗时、日志和拓扑上下文的失败报告。
- **多团队组织**：希望各团队维护自己的 profile bundle，同时共享一个通用沙箱核心。

## 使用场景

- 为某个接口节点下的所有用例生成回归报告。
- 按标签、owner、优先级、状态或接口节点执行已维护用例集合。
- 回归前先审计已维护集合的最新 Store 覆盖，判断哪些用例需要重跑。
- 按工作流顺序执行回归，并保留每一步 Evidence。
- 让 agent 先发现可测目标，再选择要执行的测试。
- 把外部 profile bundle 发布到本地 Store，便于审阅和回放。
- 查询失败用例详情，而不用重跑整个工作流。
- 把团队测试数据留在外部配置中，避免污染开源核心。

## 快速开始

安装依赖并验证仓库：

```sh
npm ci
./bin/otsandbox.sh version
npm run demo:api-case
npm run release-check
```

`demo:api-case` 会启动一个临时本地 HTTP 服务，执行
`examples/api-cases/create-item.json`，并打印 Evidence 目录。`release-check`
会运行空白检查、生成态检查、核心守卫、Go 测试、demo、React build 和无头浏览器冒烟。

创建本地 Store 并发布一个外部配置包：

```sh
tmpdir=$(mktemp -d)
store="$tmpdir/store.sqlite"
profile_dir="$tmpdir/sample-profile"

./bin/otsandbox.sh store upgrade --store-url "$store"
./bin/otsandbox.sh profile init --output "$profile_dir" --id sample
./bin/otsandbox.sh profile install --from "$profile_dir"
./bin/otsandbox.sh profile verify --profile sample --store-url "$store"
```

启动工作台：

```sh
./bin/otsandbox.sh serve \
  --profile sample \
  --store-url "$store" \
  --host 127.0.0.1 \
  --port 18191
```

然后打开 `http://127.0.0.1:18191/`。

## Agent 标准流程

Open Test Sandbox 的设计原则是：agent 不应该在提示词里写死目标 ID。

```sh
./bin/otsandbox.sh interface-node discover \
  --profile sample \
  --store-url "$store" \
  --filter "query" \
  --json

./bin/otsandbox.sh case discover \
  --profile sample \
  --store-url "$store" \
  --tag smoke \
  --status active \
  --json

./bin/otsandbox.sh case suite report \
  --profile sample \
  --store-url "$store" \
  --tag smoke \
  --status active \
  --base-url http://127.0.0.1:8080 \
  --output-dir "$tmpdir/reports/smoke-suite" \
  --json

./bin/otsandbox.sh case suite coverage \
  --profile sample \
  --store-url "$store" \
  --tag smoke \
  --status active \
  --json

./bin/otsandbox.sh case suite inspect \
  --profile sample \
  --store-url "$store" \
  --tag smoke \
  --status active \
  --json

./bin/otsandbox.sh case suite plan \
  --profile sample \
  --store-url "$store" \
  --tag smoke \
  --status active \
  --action run \
  --action rerun \
  --request-id change-001 \
  --json

./bin/otsandbox.sh interface-node case report \
  --node NODE_ID \
  --profile sample \
  --store-url "$store" \
  --base-url http://127.0.0.1:8080 \
  --output-dir "$tmpdir/reports" \
  --json
```

工作流也遵循同样模式：

```sh
./bin/otsandbox.sh workflow discover --profile sample --store-url "$store" --json
./bin/otsandbox.sh workflow report --workflow WORKFLOW_ID --profile sample --store-url "$store" --json
```

报告里可以包含失败用例。失败用例不是报告生成失败，而是沙箱成功保留了需要审阅的失败细节。

## 架构

```text
外部配置包
  -> 审计 / 验证 / 发布
  -> SQLite Store 与 catalog read-model
  -> CLI discovery、Control plane API、React 工作台
  -> 用例与工作流执行
  -> Evidence 文件与 Store 索引
  -> JSON / HTML 报告
  -> 失败用例详情 API
```

核心模块保持通用：

- `cmd/otsandbox/`：CLI 入口和命令编排。
- `internal/store/`：Store 契约和运行记录。
- `internal/store/sqlite/`：默认本地 Store 后端。
- `internal/profile/`：profile schema 和 loader。
- `internal/controlplane/`：HTTP API、工作台数据、报告和 Evidence 视图。
- `internal/apicase/`：HTTP 用例 runner 和 Evidence 写入。
- `control-plane/frontend/`：React 工作台源码。
- `control-plane/static/`：`otsandbox serve` 提供的静态工作台资源。

## 文档

| 文档 | 内容 |
| --- | --- |
| [Quick Start](docs/quickstart.md) | 首次本地运行、Store 初始化、profile 安装和工作台启动。 |
| [Backend Capabilities](docs/backend-capabilities.md) | Store、配置发布、目标发现、执行、报告、Evidence、API 和发布守卫。 |
| [Share Kit](docs/share-kit.md) | 项目 tagline、短介绍、demo 脚本和传播文案。 |
| [Roadmap](docs/roadmap.md) | 公开迭代主题和适合贡献的里程碑。 |
| [Profile Authoring](docs/profile-authoring.md) | 如何把团队测试资产维护在核心仓库之外。 |
| [Profile Format](docs/profile-format.md) | manifest 字段、拆分资产、审计、安装、打包、导入和验证。 |
| [API Case Format](docs/api-case-format.md) | 可运行 HTTP 用例 JSON 和 Evidence 输出契约。 |
| [CLI and API Contracts](docs/cli-api-contracts.md) | agent/CI 目标发现、报告、异步批量和失败证据查询。 |
| [Release Checklist](docs/release-checklist.md) | 发布前的本地和 CI 门禁。 |
| [Visual Overview](docs/core-capabilities-skills-goals.html) | 双语能力地图、API 面、数据流和迭代目标。 |

## 项目原则

- 默认开发体验必须本地、轻量、可复现。
- SQLite 是默认 Store。
- 团队或业务数据必须放在外部配置包中。
- Store 记录是索引和运行事实，不是配置源。
- Evidence、报告、日志和本地数据库都是运行产物。
- agent 必须先发现目标，再执行报告。
- CLI、API、profile、Store 或报告契约变化时，要同步更新文档。

## 当前状态

项目仍是 pre-1.0，但已经具备完整本地闭环：

- profile 生命周期：init、install、pack、audit、verify、import、publish；
- Store 生命周期：status、upgrade、运行索引、契约测试；
- 维护能力：API 用例元数据、可搜索用例目录和集合覆盖；
- 执行能力：单 API 用例、已维护用例集合、接口节点报告、工作流报告；
- Evidence：请求、响应、断言、摘要、日志、拓扑、耗时；
- 工作台：基于 Control plane API 的本地 React 页面；
- 发布门禁：`npm run release-check`。

后续重点是 profile 作者体验、更强的后处理调度、可选团队 Store 后端，以及更丰富的公开示例。

## 贡献

提交变更前请运行完整本地门禁：

```sh
npm run release-check
```

更多信息见 [CONTRIBUTING.md](CONTRIBUTING.md)、[SECURITY.md](SECURITY.md)
和 [docs/release-checklist.md](docs/release-checklist.md)。Open Test Sandbox
使用 [Apache License 2.0](LICENSE)。
