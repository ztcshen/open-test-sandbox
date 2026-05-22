# AgentTestBench Context

This file defines the shared language for the project.

## Language

**Sandbox**:
A local or hosted environment where tests can run against real or simulated
services while preserving reproducible Evidence.

**Current State**:
The active services, Workflows, API Cases, fixtures, runner settings, runtime
facts, and execution records known to the sandbox.

**Workflow**:
A template-driven sequence of testable Steps.

**Step**:
One observable action inside a Workflow.

**API Case**:
A single runnable interface test with request rendering, fixtures, assertions,
and Evidence.

**Fixture**:
Input or precondition data needed to run a Case or Workflow.

**Evidence**:
The request, response, logs, database facts, screenshots, and other records that
explain what happened during a run.

**Store**:
The database backend used for current state, runtime indexes, run records,
catalog read-models, and baseline gate state.

**Template Package**:
An optional reviewable file representation used only for import, export,
sharing, or migration.

## Relationships

- A Sandbox exposes current state through APIs and UI.
- Current State contains Workflows, API Cases, fixtures, service metadata, and
  runtime facts.
- A Workflow contains Steps.
- A Step may run or reference an API Case.
- Runs write Evidence and Store records.
- Template Packages are optional transfer artifacts; Store records are the
  active local facts.
