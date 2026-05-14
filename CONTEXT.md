# Open Test Sandbox Context

This file defines the shared language for the project.

## Language

**Sandbox**:
A local or hosted environment where tests can run against real or simulated
services while preserving reproducible Evidence.

**Profile**:
A bundle of services, Workflows, API Cases, fixtures, and runner settings for
one domain or system under test.

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
The database backend used for runtime indexes, run records, imported profile
indexes, and baseline gate state.

**Bundle**:
A reviewable file representation of configuration assets.

## Relationships

- A Sandbox loads one or more Profiles.
- A Profile contains Workflows, API Cases, fixtures, and service metadata.
- A Workflow contains Steps.
- A Step may run or reference an API Case.
- Runs write Evidence and Store records.
- Bundles are source assets; Store records are generated or imported indexes.
