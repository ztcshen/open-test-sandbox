---
name: agent-testbench-feedback
description: Register AgentTestBench usability, diagnostics, workflow, API, CLI, Store-first, or evidence feedback discovered while using /Users/zlh/codes/agent-testbench, without writing temporary notes under /tmp.
---

# AgentTestBench Feedback

Use this skill when an AgentTestBench session discovers a product, CLI, API,
workflow, Store, evidence, or documentation improvement that is not part of the
current implementation slice.

## Rules

- Keep AgentTestBench local/GitHub-only. Do not post feedback to Multica or
  Mandao unless the current user explicitly asks for that external action.
- Register durable feedback in this skill folder, not in `/tmp`.
- Keep each entry actionable: symptom, evidence, impact, suggested change, and
  current status.
- If the current slice fixes the issue, mark it `fixed` and include the file or
  test that verifies it.
- If the issue is larger than the current slice, mark it `backlog` with a
  concrete next step.

## Register Feedback

Prefer the bundled script so entries stay consistent:

```bash
python3 skills/agent-testbench-feedback/scripts/register_feedback.py \
  --title "Short feedback title" \
  --area "cli|environment|case-run|evidence|store|docs|skill" \
  --severity "P1|P2|P3" \
  --source "session note, command, or user report" \
  --evidence "Observed behavior and command output summary" \
  --suggestion "Concrete improvement or next step"
```

The script appends to `skills/agent-testbench-feedback/feedback.md`.

## Review Feedback

Before starting broad usability work, read
`skills/agent-testbench-feedback/feedback.md` and pick a small verified slice.
Update the matching entry status after fixing or intentionally deferring it.
