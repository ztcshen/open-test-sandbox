# Roadmap / 路线图

Open Test Sandbox is pre-1.0. The roadmap focuses on making the project easier
to try, easier to extend, and safer for real team adoption.

Open Test Sandbox 目前仍是 pre-1.0。路线图重点是降低试用门槛、提升扩展性，并让
真实团队接入更安全。

## Now / 当前

- Keep the core repository generic and free of bundled team import bundles.
- Maintain a green `npm run release-check` gate.
- Keep the headless smoke focused on the core workflow path: enter from the
  workbench, run the workflow button flow, confirm green steps, inspect step
  Evidence, and require real SkyWalking topology instead of fallback diagrams.
- Improve README, bilingual docs, and first-run experience.
- Keep CLI and API contracts documented as they change.

- 保持核心仓库通用，不内置团队 import bundle。
- 保持 `npm run release-check` 门禁稳定通过。
- 保持 headless smoke 覆盖核心 Workflow 路径：从工作台进入，点击运行
  Workflow，确认节点绿色，查看 step Evidence，并要求真实 SkyWalking 拓扑，
  不使用 fallback 假图。
- 完善 README、双语文档和首次运行体验。
- CLI/API 契约变化时同步更新文档。

## Next / 下一阶段

- Add richer generic example import bundles that remain safe for open-source use.
- Improve import bundle authoring ergonomics and validation messages.
- Make post-process tasks for topology, logs, and reports easier to inspect,
  including clear passed, skipped, and failed reasons for Evidence collection.
- Expand report templates while keeping them compact and table-first.
- Add more focused smoke checks for CLI report generation.

- 增加更丰富但仍然通用的公开示例 import bundle。
- 改善 import bundle 作者体验和校验错误信息。
- 让拓扑、日志、报告等后处理任务更容易查看，并清晰展示 Evidence 采集的
  成功、跳过和失败原因。
- 扩展报告模板，同时保持紧凑、表格优先。
- 增加 CLI 报告生成的聚焦冒烟测试。

## Later / 后续

- Add an optional team Store backend while keeping SQLite as the default.
- Publish versioned releases and binary artifacts.
- Provide a plugin-style import bundle bundle workflow for teams.
- Add stronger redaction guidance for Evidence and reports.
- Build a small public demo site or recorded walkthrough.

- 增加可选团队 Store 后端，同时保持 SQLite 默认。
- 发布版本化 release 和二进制产物。
- 提供面向团队的 import bundle bundle 插件式工作流。
- 补充 Evidence 和报告脱敏指南。
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
