# Roadmap / 路线图

Open Test Sandbox is pre-1.0. The roadmap focuses on making the project easier
to try, easier to extend, and safer for real team adoption.

Open Test Sandbox 目前仍是 pre-1.0。路线图重点是降低试用门槛、提升扩展性，并让
真实团队接入更安全。

## Now / 当前

- Keep the core repository generic and free of bundled team template packages.
- Maintain a green `npm run release-check` gate.
- Treat the product as a CLI-first, SQL Store-first testing workbench.
  PostgreSQL and MySQL are supported product Store engines; teams should choose
  the engine that matches their operational environment. Each isolation
  boundary uses its own Store database, for example a private `local-personal`
  database and a shared `team-verified` database. Docker runtime management
  stays local for now.
- Make the daily core flow work from CLI and the local workbench: configure
  Store, register and discover Environment Catalog entries, inspect and
  bootstrap an environment, verify it, publish it to verified discovery, register
  a local code service by repository path and branch, register interfaces,
  register workflows, add or edit API cases, run cases or workflows, and inspect
  reports, Evidence, and stored topology, with real SkyWalking validation when
  a live endpoint is configured.
- Keep the HTTP API as the local control-plane surface for the workbench,
  automation, and agents. Do not require every offline authoring command to be
  an API; require API parity for daily testing operations.
- Keep the headless smoke focused on the core workflow path: enter from the
  workbench, run the workflow button flow, confirm green steps, inspect step
  Evidence, and require provider-labelled topology evidence. Real SkyWalking
  proof is gated by a live endpoint; otherwise the topology path must report
  unavailable, failed, or skipped status rather than a fake diagram.
- Improve README, bilingual docs, and first-run experience.
- Keep CLI and API contracts documented as they change.

- 保持核心仓库通用，不内置团队 template package。
- 保持 `npm run release-check` 门禁稳定通过。
- 将产品定位为 CLI-first、SQL Store-first 的测试工作台。PostgreSQL 与 MySQL
  都是产品 Store 引擎；团队按自己的运维环境选择。每个隔离边界使用独立 Store
  database，例如个人 `local-personal` 和团队共享 `team-verified`；Docker runtime
  暂时只在本地管理。
- 让日常核心流程能通过 CLI 和本地工作台完成：配置 Store，按本地仓库路径和
  分支注册代码服务，登记和发现环境目录，查看、初始化、验收环境并发布到
  verified 发现列表，登记接口，登记工作流，新增或修改 API 用例，执行用例或
  工作流，并查看报告、Evidence 和已存储拓扑；配置真实 endpoint 时验证真实
  SkyWalking 拓扑。
- HTTP API 保留为本地 control-plane，服务工作台、自动化和 agent；离线作者工具
  不要求全部 API 化，但日常测试操作必须保持 API parity。
- 保持 headless smoke 覆盖核心 Workflow 路径：从工作台进入，点击运行
  Workflow，确认节点绿色，查看 step Evidence，并要求真实 SkyWalking 拓扑，
  不使用假拓扑图。真实 SkyWalking 证明由 live endpoint 触发；未配置时拓扑
  路径必须明确显示 unavailable、failed 或 skipped。
- 完善 README、双语文档和首次运行体验。
- CLI/API 契约变化时同步更新文档。

## Next / 下一阶段

- Add richer generic example template packages that remain safe for open-source use.
- Improve template package authoring ergonomics and validation messages.
- Continue improving post-process task inspection for topology, logs, and
  reports. API and CLI task payloads now include clear passed, skipped, and
  failed Evidence collection reasons through `outcome`, `reason`, and
  `displayStatus`.
- Expand report templates while keeping them compact and table-first.
- Add more focused smoke checks for CLI report generation.

- 增加更丰富但仍然通用的公开示例 template package。
- 改善 template package 作者体验和校验错误信息。
- 让拓扑、日志、报告等后处理任务更容易查看，并清晰展示 Evidence 采集的
  成功、跳过和失败原因。
- 扩展报告模板，同时保持紧凑、表格优先。
- 增加 CLI 报告生成的聚焦冒烟测试。

## Later / 后续

- Document optional organization-owned MySQL sign-off evidence with a real
  MySQL Store DSN, live SkyWalking GraphQL endpoint, and trace ids for every
  configured workflow step.
- Deepen clean-machine restore evidence across more Docker Compose stacks and
  middleware combinations.
- Publish versioned releases and binary artifacts.
- Provide a plugin-style template package workflow for optional
  import/export/review artifacts.
- Continue broadening redaction guidance and raw-artifact opt-in controls for
  Evidence and reports.
- Build a small public demo site or recorded walkthrough.

- 使用真实 MySQL Store DSN、live SkyWalking GraphQL endpoint 和覆盖配置工作流的
  trace id 记录组织自有环境的可选验收证据。
- 在更多 Docker Compose 栈和中间件组合上深化干净机器恢复证据。
- 发布版本化 release 和二进制产物。
- 为可选导入、导出、审阅资产提供插件式模板包工作流。
- 继续完善 Evidence/报告脱敏指南和 raw artifact 显式查看控制。
- 建立小型公开 demo 站点或录屏 walkthrough。

## Good First Contributions / 适合首次贡献

- Improve wording in bilingual docs.
- Add small synthetic API case examples.
- Add focused tests around CLI report JSON shape.
- Improve issue templates and troubleshooting docs.
- Add screenshots or terminal casts for the quick start.

- 改进双语文档表述。
- 增加小型合成 API case 示例。
- 为 CLI 报告 JSON 结构补聚焦测试。
- 改进 issue 模板和排障文档。
- 为快速开始补截图或终端录屏素材。
