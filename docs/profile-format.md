# Profile Bundle Format

A profile bundle is a reviewable directory of configuration assets. The minimum
bundle contains a `profile.json` manifest.

```json
{
  "id": "empty",
  "displayName": "Empty Profile",
  "description": "A neutral profile with no services, workflows, cases, or fixtures.",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "fixtures": []
}
```

## Manifest Fields

- `id`: stable profile identifier.
- `displayName`: human-readable profile name.
- `description`: optional profile summary.
- `services`: systems or dependencies involved in the profile.
- `workflows`: template-driven sequences of testable steps.
- `interfaceNodes`: observable interfaces that cases can target.
- `apiCases`: runnable interface tests.
- `fixtures`: input or precondition data for cases and workflows.

Configuration remains file-first. Store records are generated runtime indexes,
not the source of truth for profile assets.
