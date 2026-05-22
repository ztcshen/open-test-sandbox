# Clean-Machine Docker Restore Test Plan

## Scope

This plan describes the generic clean-machine Docker restore model for a
Store-backed Environment Catalog entry anchored to that environment's configured
verification workflow. Private validation environments may use their own
workflow ids, component names, and step counts, but public documentation should
describe the reusable model rather than a specific organization's suite.

The sandbox Store is not part of the restored Docker target. The Store must be
reachable before restore starts and must remain outside the Docker lifecycle
that starts, stops, or removes target application containers.

## Current Proven State

- Store inspection, restore planning, and verification are done through
  `agent-testbench environment ...` CLI/API surfaces, not direct database SQL.
- The SQL Store schema now has `environment_components`,
  `component_dependencies`, and `component_config_assets` as the component graph
  model. Workflow run records can link back to an environment through
  `runs.environment_id`.
- `environment restore` can dry-run or execute remote repository preparation,
  Store-generated startup/config assets, Docker Compose pull/build/up, recorded
  health gates, and the configured verification workflow.
- Verified publication remains gated by a passed workflow run, indexed Evidence,
  and complete real SkyWalking topology in the selected Store.

## Remaining Public Proof Gaps

The private validation path has exercised the full clean-machine shape, but the
open-source repository still needs reusable public evidence and examples:

- a small public example environment with remote repositories that anyone can
  clone;
- compact component-owned assets for middleware and application startup;
- a documented restore transcript that starts from an empty workspace and a
  Compose-scoped Docker cleanup;
- a workflow acceptance report with Evidence and real or explicitly unavailable
  topology status, depending on whether a live SkyWalking endpoint is provided.

## Component Graph Store Model

The Store model should be a component graph, not a middleware-vs-service split.
Every runtime unit in a suite is a component: middleware, platform service,
mock, observability service, support process, or application service.

- `environment_components`: one row per runtime unit in the suite.
- `component_dependencies`: directed edges from a consumer component to a
  provider component.
- `component_config_assets`: config assets owned by the consumer component and
  targeted at the provider component or at the consumer's own runtime.
- `component_connection_profiles`: provider-owned connection facts that can be
  projected into consumers during restore.

The model is intentionally close to mature component/resource systems:

- Dapr treats middleware integrations as interchangeable components with
  component-specific metadata and optional secret references.
- Kubernetes separates deployable workload shape from ConfigMap and Secret
  material that is mounted or injected into the workload.
- Service Binding treats provider connection details as bindable data consumed
  by applications.
- Backstage's system model separates systems, components, resources, and
  dependency relationships for catalog discovery.
- Terraform/OpenTofu resource graph design is a useful execution reference:
  build a graph, validate cycles, then walk nodes when dependencies are ready.

The ownership rule is: the provider exposes capability; the consumer owns the
configuration it needs in order to consume that capability.

Examples:

- `order-api -> mysql`: `order-api` owns its database name, DDL, and seed assets.
- `order-api -> config-service`: `order-api` owns app id, namespace, and
  key/value assets.
- `redis-sentinel -> redis-master`: Sentinel owns the monitor configuration.
- `grafana -> loki`: Grafana owns datasource provisioning that points to Loki.
- `promtail -> loki`: Promtail owns its push endpoint configuration.
- `skywalking-ui -> skywalking-oap`: UI owns the OAP address setting.
- `skywalking-oap -> storage`: OAP owns its storage backend settings.
- `xxl-job-admin -> mysql`: XXL-job owns its tables and DB connection config.

MySQL DDL and seed SQL are therefore not owned by the MySQL component. They are
owned by the component that needs those schemas. Configuration middleware
follows the same rule: the provider exposes config-service capability, while
each consuming component owns its app id, namespace, and key/value assets.

The current SQL Store schema uses `component_dependencies` and
`component_config_assets` directly. Legacy service-shaped metadata is treated as
compatibility input and should be projected into the component graph for daily
restore paths.

### Dependency Edge Semantics

The Store edge direction remains `consumer -> provider` because it answers the
question "which provider does this component consume?" Restore then projects
blocking edges into an execution graph as `provider -> consumer` so topological
ordering starts shared providers before their consumers.

Each dependency edge needs an explicit phase and capability:

- `prepare`: asset generation or repository preparation must happen before the
  consumer can be prepared.
- `startup`: provider must be started before the consumer can start.
- `readiness`: consumer may start, but acceptance cannot proceed until the
  provider is healthy and the consumer can reach it.
- `runtime`: bidirectional or late-bound runtime traffic. This edge documents
  the relationship but is excluded from startup topological ordering.
- `acceptance`: workflow/report validation requires this provider, for example
  real SkyWalking topology collection.

Blocking phases are `prepare`, `startup`, `readiness`, and `acceptance`.
`runtime` edges may contain cycles only when all involved components have
explicit health probes, bounded waits, and reportable readiness gates.

### Graph Algorithm Boundary

Cycle detection and topological ordering are core correctness logic, so the
project must not hand-write DFS, strongly connected component, or topological
sort algorithms. Implement a small domain adapter over
`gonum.org/v1/gonum/graph` and `gonum.org/v1/gonum/graph/topo` instead:

- Use `simple.DirectedGraph` or a similarly small Gonum directed graph type for
  the projected restore graph.
- Use `topo.SortStabilized` for deterministic provider-before-consumer order.
- Use `topo.DirectedCyclesIn` to produce reportable cycle paths for blocking
  edges.
- Use `topo.TarjanSCC` only when the report needs grouped strongly connected
  components, not as a project-owned algorithm.

AgentTestBench-owned code should do only domain translation:

- read components and dependencies through Store APIs,
- split edges by phase,
- project blocking edges from `consumer -> provider` to
  `provider -> consumer`,
- call Gonum topology functions,
- translate sorted nodes and cycle paths into CLI/API/UI restore preflight
  output.

Acceptance criteria for this adapter:

- a blocking cycle fails preflight with the exact component path;
- a pure runtime cycle is allowed only when all readiness gates are present;
- mixed graphs ignore runtime edges for startup ordering but still report them;
- ordering is stable across repeated runs for the same Store data;
- all graph validation is reachable through CLI/API, never by direct Store SQL.

## Three-Layer Test Path

1. Non-destructive CLI dry-run:
   - Inspect the environment through CLI/API.
   - Confirm compact Store metadata size.
   - Confirm source repos are remote Git URLs.
   - Confirm component graph blocking edges pass the Gonum-backed cycle check
     and topological order check.
   - Confirm runtime cycles, if any, have explicit health probes and bounded
     readiness gates.
   - Confirm startup assets are owned by consumer components and are either
     generated by Store metadata or already present in the restore workspace.

2. Isolated workspace preparation:
   - Clone all component repositories into a fresh workspace.
   - Write Store-backed component assets in Gonum-derived dependency order.
   - Stop before Docker with `--prepare-repos-only`.
   - Verify no application runtime files, logs, Docker images, build cache, or
     Evidence payloads were written into the Store.

3. Operator-approved Docker simulation:
   - Capture `docker compose ps`, `docker compose images`, and `docker compose
     config`.
   - Only after review, run the Compose-scoped cleanup for this environment.
   - Start provider components first when possible, then consumer components,
     using explicit readiness gates for components with runtime cycles.
   - Wait for all recorded component health probes.
   - Trigger async acceptance with the bound verification workflow.
   - Publish verified only when Evidence and real SkyWalking topology are both
     complete.

## Storage Boundary

The selected SQL Store stores compact environment metadata and deterministic
startup text such as DDL, seed SQL, launch scripts, certificates, keys, and
configuration assets. It must not store Docker images, code repositories, Maven
caches, runtime databases, runtime logs, Evidence payloads, or large binaries.
There are no per-kind limits for deterministic text assets. If an inline asset,
environment definition/summary, or component graph crosses 1 MB, the Store write
must block and report the exact offending field, asset, or graph-size reason.

The current 5.6 MB WireMock dependency jar is not a Store candidate. It should
come from a remote artifact, a purpose-built image, or a remote repo build step.
