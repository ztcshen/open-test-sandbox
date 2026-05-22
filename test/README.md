# Test Asset Layout

Open Test Sandbox keeps fast, generic Go unit tests next to the package they
exercise. Shared test assets belong here when they are safe to publish.

- `fixtures/`: public sample profiles, API cases, Store payloads, and traces.
- `golden/`: public expected reports and serialized outputs.
- `integration/`: public black-box CLI, HTTP API, and Store integration tests.

Private validation assets must not be committed to this repository. Put real
merchant data, certificates, private profiles, target environment addresses,
and live Evidence samples in `test-private/` or a separate private validation
repository.
