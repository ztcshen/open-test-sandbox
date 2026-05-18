# Backend Capabilities / 后端能力总览

Open Test Sandbox backend is a local-first control plane for profile-driven
integration testing. It is built around a generic Store interface, a SQLite
default backend, a CLI surface for agents and CI, and HTTP APIs for the
workbench.

Open Test Sandbox 的后端是一套本地优先的测试控制平面，用外部配置包驱动集成测试。
它由通用 Store 接口、默认 SQLite 后端、面向 agent/CI 的 CLI、以及面向工作台的
HTTP API 共同组成。

## Capability Map / 能力地图

| Area / 能力域 | What it does / 能力简介 | Primary surfaces / 主要入口 |
| --- | --- | --- |
| Store / 本地事实库 | Holds schema state, profile indexes, run records, case run records, Evidence indexes, baseline gates, timing summaries, runtime logs, topology records, and post-process tasks. 保存 schema、配置索引、运行记录、用例运行、证据索引、基线、耗时、运行日志、拓扑记录和后处理任务。 | `internal/store`, `internal/store/sqlite`, `store status`, `store upgrade` |
| Profile lifecycle / 配置包生命周期 | Creates, installs, packs, audits, turns audit issues into repair actions, verifies, imports, and publishes external profile bundles. Runtime files and local databases are filtered out. 创建、安装、打包、审计、把审计问题转换成修复行动、验证、导入和发布外部配置包，并过滤运行产物和本地数据库。 | `profile init`, `profile install`, `profile pack`, `profile audit`, `profile audit-plan`, `profile verify`, `/api/profile/*` |
| Catalog read-model / 目录视图 | Converts profile assets into service, workflow, interface node, case, template, fixture, dependency, and binding views for the UI and agent commands. 将配置资产转换为服务、工作流、接口节点、用例、模板、夹具、依赖和绑定视图。 | `/api/catalog`, `/api/dashboard`, `/api/profile/catalog-index` |
| Discovery / 目标发现 | Lets agents search interface nodes, workflows, and maintained API cases before running reports. This avoids hardcoded prompt identifiers. 让 agent 先搜索接口节点、工作流和已维护用例，再用精确 ID 执行报告，避免提示词写死目标。 | `interface-node discover`, `workflow discover`, `case discover` |
| Case maintenance / 用例维护 | Indexes case description, tags, priority, owner, status, runnable file presence, execution configuration, readiness issues, latest run state, stability signals, impact reasons, priority scores, one-call suite briefs, authoring quality gaps, quality repair plans, quality report artifacts, and executable plans for review, assignment, coverage, prioritization, impact analysis, and suite execution. Agents can draft and apply maintained cases to external profile bundles without direct Store writes. CLI reports, Control plane coverage, stability, priority, brief, quality, quality plan, quality report, inspection, planning, impact, impact execution, and batch rerun selectors share one backend rule module. 索引用例说明、标签、优先级、owner、状态、可运行文件、执行配置、就绪问题、最新运行状态、稳定性信号、影响原因、优先级评分、单次查询集合摘要、维护质量缺口、质量修复计划、质量报告产物和可执行计划，便于审阅、分派、覆盖检查、优先级排序、影响面分析和集合执行；agent 可以把已维护用例草稿应用到外部 profile bundle，不直接写 Store；CLI 报告、控制平面覆盖率、稳定性、优先级、摘要、质量、质量计划、质量报告、检查、计划、影响面、影响面执行和批量重跑选择器共用同一套后端规则模块。 | `internal/casesuite`, `interface-node case draft`, `interface-node case apply`, `case discover`, `case suite quality`, `case suite quality-plan`, `case suite quality-report`, `case suite stability`, `case suite priority`, `case suite brief`, `case suite inspect`, `case suite plan`, `case suite impact`, `case suite impact-report`, `case suite coverage`, `case suite report`, Store catalog, profile API case metadata |
| API case execution / 用例执行 | Runs a single HTTP case, renders requests, checks assertions, writes Evidence files, and optionally indexes the run into Store. 执行单个 HTTP 用例，渲染请求、校验断言、写入 Evidence，并可索引到 Store。 | `case run`, `/api/cases/run`, `/api/test-kit/run` |
| Suite and interface reports / 集合与接口报告 | Runs exact case ids, cases selected by maintenance metadata, impact signals, or all cases attached to one interface node, then returns JSON plus temporary HTML, JUnit XML, artifact manifests, and compact failure summaries. Failed cases remain part of the report. 按精确用例 ID、维护条件、影响面线索，或某个接口节点下的全部用例执行，返回 JSON、临时 HTML、JUnit XML、产物清单和紧凑失败摘要；失败用例保留在报告内。 | `case suite report`, `case suite impact-report`, `interface-node case report`, `/api/cases/batch-runs`, `/api/case/suite-impact-runs` |
| Workflow reports / 工作流报告 | Runs workflow-bound steps in configured order and records per-step case run details. 按配置顺序执行工作流步骤，并记录每一步的用例运行详情。 | `workflow report`, `/api/workflow-runs`, `/api/workflow-runs/{id}` |
| Evidence lookup / 证据查询 | Reads request, response, assertions, summaries, fixture context, stored topology, persisted logs, batch artifact manifests, and batch failure summaries for a run or case run. Report previews redact common sensitive JSON keys and URL query parameters. 按运行或 case run 查询请求、响应、断言、摘要、前置数据、拓扑、持久化日志、批量产物清单和失败摘要；报告预览会脱敏常见敏感 JSON 键和 URL 查询参数。 | `internal/redaction`, `evidence list`, `/api/case/evidence`, `/api/case-run/evidence`, `/api/cases/batch-runs/{id}/artifacts.json`, `/api/cases/batch-runs/{id}/failures.json` |
| Observability import / 观测导入 | Stores trace topology, replay/log-style Evidence, and post-process task duration/status for later review without blocking the main case response. 存储拓扑、回放/日志类证据和后处理任务耗时/状态，供后续审阅复用，避免阻塞主请求。 | `/api/trace-topology/collect`, `/api/replay/evidence`, `/api/post-process-tasks`, `evidence tasks`, post-process task records |
| Coverage and timing / 覆盖与耗时 | Shows interface or maintained-suite coverage, missing coverage, latest status, recent stability, priority ranking, elapsed time, readiness gaps, and configured timing budgets where available. 展示接口或已维护集合覆盖、缺口、最新状态、近期稳定性、优先级排序、实际耗时、就绪缺口和配置的耗时预算。 | `case suite coverage`, `case suite stability`, `case suite priority`, `case suite inspect`, `/api/case/suite-coverage`, `/api/case/suite-stability`, `/api/case/suite-priority`, `/api/case/suite-inspection`, `/api/interface-node/coverage`, `/api/interface-node/coverage-gaps`, `/api/case/timing` |
| Workbench serving / 工作台服务 | Serves the React workbench, static pages, and JSON APIs from the same local control plane. 从同一个本地控制平面提供 React 工作台、静态页面和 JSON API。 | `serve`, `control-plane/static`, `control-plane/frontend` |
| Release guardrails / 发布守卫 | Prevents generated state and source-domain terms from entering the core repository, then runs tests and browser smoke checks. 防止生成态和来源域词汇进入核心仓库，并运行测试与浏览器冒烟。 | `npm run release-check`, `tools/guardrails/check_no_source_domain_core.sh` |

## Data Flow / 数据流

1. A profile bundle is authored outside the core repository.
   配置包在核心仓库外维护。
2. The profile is installed, audited, and published into the local Store.
   配置包被安装、审计，并发布到本地 Store。
3. Catalog/read-model APIs expose the profile to the workbench and CLI.
   Catalog/read-model API 将配置暴露给工作台和 CLI。
4. An agent discovers a target and runs a case, interface-node report, or
   workflow report.
   agent 先发现目标，再执行单用例、接口节点报告或工作流报告。
5. The runner writes local Evidence and indexes run facts into Store.
   runner 写入本地 Evidence，并把运行事实索引到 Store。
6. Reports and detail APIs read Store plus Evidence files to show results,
   failed assertions, request/response data, logs, and topology.
   报告和详情 API 读取 Store 与 Evidence，展示结果、失败断言、请求响应、日志和拓扑。

The Store is an index and runtime fact database. Profile files and Evidence
files remain separate source and runtime artifacts.

Store 是索引和运行事实库；配置文件和 Evidence 文件分别是源资产和运行产物。

## Control Plane API Groups / 控制平面 API 分组

### Profile and Catalog / 配置与目录

- `GET /api/profile`: active profile summary. 当前配置摘要。
- `GET /api/profile/assets`: active profile asset summary. 当前配置资产摘要。
- `GET /api/profile/installed`: installed profile bundles under the profile
  home. 配置 home 下已经安装的配置包。
- `POST /api/profile/install`: install a profile directory or archive. 安装配置目录或归档包。
- `POST /api/profile/import`: publish a profile into Store/read-models. 发布配置到 Store/read-model。
- `POST /api/profile/audit-plan`: convert audit issues into stable repair
  actions. 将审计问题转换成稳定修复行动项。
- `POST /api/profile/verify`: audit, publish, and run configured acceptance
  checks. 审计、发布并执行配置的验收检查。
- `GET /api/profile/catalog-index`: latest Store catalog index. 最新 Store catalog 索引。
- `GET /api/catalog`: service, workflow, interface node, case, fixture,
  template, dependency, and binding read-models. 服务、工作流、接口节点、用例、夹具、模板、依赖和绑定视图。
- `GET /api/dashboard`: dashboard summary derived from profile and Store.
  由配置和 Store 派生的看板摘要。

### Interface Nodes and Coverage / 接口节点与覆盖

- `GET /api/interface-nodes`: list interface nodes, with optional service or
  operation filtering. 接口节点列表，可按服务或操作过滤。
- `GET /api/interface-node`: detail for one interface node, optionally scoped
  by workflow run and step context. 单个接口节点详情，可按工作流运行和步骤上下文限定。
- `GET /api/interface-node/coverage`: coverage view for an optional workflow.
  可选工作流下的覆盖视图。
- `GET /api/interface-node/coverage-gaps`: missing or incomplete coverage for
  an optional workflow. 可选工作流下的覆盖缺口。

### Runs and Workflows / 运行与工作流

- `GET /api/runs`: recent run index. 最近运行索引。
- `POST /api/workflow-runs`: save a workflow run snapshot. 保存工作流运行快照。
- `GET /api/workflow-runs/{id}`: workflow run detail. 工作流运行详情。
- `GET /api/workflow-runs/step`: step detail inside a run. 某次运行中的步骤详情。
- `GET /api/workflow-runs/latest-step`: latest step result for a workflow and
  step id. 某工作流和步骤的最新结果。
- `GET /api/workflow-audit`: workflow reference integrity and acceptance view.
  工作流引用完整性和验收视图。

### Cases, Reports, and Evidence / 用例、报告与证据

- `GET /api/cases/capabilities`: runnable case capability matrix. 可运行用例能力矩阵。
- `POST /api/cases/run`: run one case from profile configuration. 运行配置中的一个用例。
- `POST /api/cases/batch-runs`: start an asynchronous batch for interface
  nodes, exact case ids, one workflow, or a maintained suite selector.
  为接口节点、精确用例 ID、工作流或已维护集合条件启动异步批量执行。
- `GET /api/cases/batch-runs/{id}`: batch JSON report. 批量运行 JSON 报告。
- `GET /api/cases/batch-runs/{id}/report.html`: batch HTML report. 批量运行 HTML 报告。
- `GET /api/cases/batch-runs/{id}/report.junit.xml`: batch JUnit XML
  report. 批量运行 JUnit XML 报告。
- `GET /api/cases/batch-runs/{id}/artifacts.json`: batch artifact manifest
  with report URLs, JUnit XML, case detail links, and Evidence paths.
  批量运行产物清单，包含报告 URL、JUnit XML、用例详情链接和 Evidence 路径。
- `GET /api/cases/batch-runs/{id}/failures.json`: compact failed-case
  summary with detail URLs, Evidence paths, elapsed time, and assertion errors.
  失败用例紧凑摘要，包含详情 URL、Evidence 路径、耗时和断言错误。
- `GET /api/case/runs`: case run list. 用例运行列表。
- `GET /api/case/evidence`: Evidence detail by run/case/step context.
  按运行、用例或步骤上下文查询 Evidence 详情。
- `GET /api/case-run/evidence`: Evidence detail by case run id.
  按 case run id 查询 Evidence 详情。
- `GET /api/case/timing`: elapsed time and budget summary. 实际耗时和预算摘要。
- `GET /api/case/incomplete-batches`: incomplete case batch coverage.
  未完成的用例批次覆盖情况。
- `GET /api/case/suite-coverage`: maintained suite latest passed, failed, and
  not-run status by selector. 按维护条件查询用例集合最新通过、失败和未运行状态。
- `GET /api/case/suite-stability`: maintained suite recent status transitions
  and unstable case flags by selector. 按维护条件查询近期状态切换和不稳定用例标记。
- `GET /api/case/suite-priority`: maintained suite priority ranking from
  impact signals, latest status, stability, and case priority metadata.
  基于影响面线索、最新状态、稳定性和用例优先级元数据查询已维护集合的优先级排序。
- `GET /api/case/suite-brief`: one-call maintained suite triage with coverage,
  readiness, stability, recommendations, blocked cases, and batch payload.
  单次查询已维护集合的覆盖、就绪、稳定性、推荐项、阻塞项和批量执行载荷。
- `GET /api/case/suite-quality`: maintained suite authoring quality gaps,
  including uncovered nodes, missing metadata, runnable source, and execution
  config. 已维护集合维护质量缺口，包括无用例接口、缺元数据、缺可运行来源和缺执行配置。
- `GET /api/case/suite-quality-plan`: stable authoring actions derived from
  suite quality gaps. 根据集合质量缺口生成稳定的维护行动项。
- `GET /api/case/suite-inspection`: maintained suite readiness, runnable
  source/config gaps, latest status, and suggested action by selector.
  按维护条件查询用例集合就绪情况、可运行来源/配置缺口、最新状态和建议动作。
- `GET /api/case/suite-plan`: deterministic executable case id plan and
  batch request payload by selector. 按维护条件生成确定性的可执行用例 ID 计划和批量请求载荷。
- `GET /api/case/suite-impact`: change-signal impact plan with matched nodes,
  workflows, cases, explanation reasons, and batch request payload.
  基于变更线索生成影响面计划，包含命中的节点、工作流、用例、解释原因和批量请求载荷。
- `POST /api/case/suite-impact-runs`: start an asynchronous batch from an
  impact plan and return report URLs immediately. 基于影响面计划启动异步批量运行，并立即返回报告 URL。

### Observability / 观测证据

- `POST /api/trace-topology/collect`: collect and store trace topology.
  采集并存储调用拓扑。
- `GET /api/post-process-tasks`: post-process task status and duration by run,
  step, case, kind, or status. 按运行、步骤、用例、类型或状态查询后处理任务状态和耗时。
- `GET /api/replay/evidence`: replay-style Evidence lookup. 查询回放类 Evidence。
- `POST /api/test-kit/run`: compatibility run endpoint for local callers.
  面向本地调用方的兼容运行入口。
- `POST /api/test-kit/run-batch`: compatibility batch endpoint. 兼容批量运行入口。

## CLI Capability Groups / CLI 能力分组

| Group / 分组 | Commands / 命令 |
| --- | --- |
| Store / Store | `store status`, `store upgrade` |
| Profile / 配置包 | `profile init`, `profile install`, `profile pack`, `profile list`, `profile inspect`, `profile audit`, `profile audit-plan`, `profile verify`, `profile import`, `/api/profile/audit-plan` |
| Config / 配置发布 | `config publish` |
| Discovery and authoring / 发现与维护 | `interface-node discover`, `workflow discover`, `case discover`, `interface-node case draft`, `interface-node case apply` |
| Reports / 报告 | `case suite report`, `case suite impact-report`, `case suite quality-report`, `interface-node case report`, `workflow report` |
| Coverage / 覆盖 | `case suite coverage`, `case suite stability`, `case suite priority`, `case suite brief`, `case suite quality`, `case suite quality-plan`, `case suite inspect`, `case suite plan`, `case suite impact`, `case incomplete-batches`, `/api/case/suite-coverage`, `/api/case/suite-stability`, `/api/case/suite-priority`, `/api/case/suite-brief`, `/api/case/suite-quality`, `/api/case/suite-quality-plan`, `/api/case/suite-inspection`, `/api/case/suite-plan`, `/api/case/suite-impact`, `/api/case/suite-impact-runs`, `/api/interface-node/coverage` |
| Execution / 执行 | `case run`, `template render` |
| Evidence / 证据 | `evidence import`, `evidence list`, `evidence tasks`, `/api/post-process-tasks` |
| Acceptance / 验收 | `baseline get`, `baseline set`, `case incomplete-batches`, `workflow audit` |
| Service / 服务 | `serve` |

## Runtime Artifacts / 运行产物

The backend writes runtime artifacts that must not be committed:

后端会写入以下运行产物，这些内容不能提交到核心仓库：

- SQLite Store files / SQLite Store 文件；
- Evidence directories / Evidence 目录；
- generated HTML/JSON/JUnit XML reports / 生成的 HTML/JSON/JUnit XML 报告；
- local logs / 本地日志；
- browser smoke output / 浏览器冒烟测试输出；
- temporary profile-home directories / 临时 profile-home 目录。

`.gitignore` and `npm run release-check` guard these boundaries.

`.gitignore` 和 `npm run release-check` 会守住这些边界。

## Current Boundaries / 当前边界

- SQLite is the supported default backend. SQLite 是当前默认支持后端。
- PostgreSQL is reserved for a future team or hosted mode. PostgreSQL 保留给后续团队/托管模式。
- Profile bundles are external configuration source, not core code. 配置包是外部配置源，不是核心代码。
- HTML reports are temporary local artifacts. HTML 报告是本地临时产物。
- Reports may contain failed cases; report generation success means the
  sandbox completed its job. 报告可以包含失败用例；能成功生成报告表示沙箱完成了自己的工作。
