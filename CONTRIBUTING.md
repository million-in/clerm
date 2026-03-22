# Contributing

## Scope

This repository contains the public CLERM compiler, binary codecs, request builder, offline inspection tools, benchmark suite, and the public registry RPC client.

The private `clerm_registry` service is a separate repository and should not be merged into this public codebase.

## Development

1. Use Go `1.24.1`.
2. Run `make build` before submitting changes.
3. Run `go test ./... -count=1` before opening a pull request.
4. Keep public APIs stable and avoid breaking binary formats without an explicit version bump.
5. Keep route details internal. Public request paths must not expose hidden schema routes.

## Pull Requests

1. Keep changes focused.
2. Add or update tests for behavior changes.
3. Update `README.md` and `CHANGELOG.md` when user-facing behavior changes.
4. Do not commit generated binaries, local schema artifacts, token files, or secrets.

## Style

1. Prefer small, reusable packages over command-local duplication.
2. Keep hot paths allocation-aware.
3. Use `log/slog` for logging and typed platform errors for failures.
