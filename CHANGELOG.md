# Changelog

## Unreleased

### Added

- Public `registryrpc` client package for CLERM registry integration.
- CLI commands for `register`, `search`, `discover`, `relationship`, `token refresh`, and `invoke`.
- CLI registry-path tests and registry client transport tests.
- MIT `LICENSE`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `go.work`, and `go.work.versions`.

### Changed

- `clerm token issue` now uses the registry RPC server instead of local signing.
- `clerm token keygen` is no longer part of the public compiler workflow.
- `clerm request` now points users to registry-issued capability tokens for protected relations.

### Fixed

- Registry startup can now auto-create signing keys and connect to TLS-backed OpenSearch endpoints.
