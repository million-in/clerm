# CLERM

CLERM is the public compiler, wire-format toolchain, and registry RPC client for schema-defined request execution.

It produces three artifacts:

- `.clermfile`: human-written schema source
- `.clermcfg`: compiled schema configuration payload
- `.clerm`: compiled single-method request payload

`.clermcfg` and `.clerm` are compact binary payloads. They are not executables.

## Scope

This repository contains:

- schema parsing and validation
- `.clermcfg` compilation and inspection
- `.clerm` request generation and inspection
- CLERM response encoding and inspection
- embeddable Go resolver runtime
- local `clerm-resolver` daemon for non-Go projects
- public registry RPC client commands
- offline decode and local resolver tools
- binary vs JSON benchmarks

The private `clerm_registry` service is a separate repository. It stores `.clermcfg`, indexes discovery metadata, manages relationships, issues invoke tokens, refreshes sessions, and routes requests using hidden schema routes.

## Schema Metadata

A schema can define registry-visible metadata under `@metadata:`:

- `description`
- `tags`
- `display_name`
- `category`

That metadata is compiled into `.clermcfg` and used by the registry for search and discovery.

## Build

```bash
make build
make build-resolver
eval "$(bin/clerm shellenv)"
clerm help
```

## Commands

```bash
clerm compile
clerm inspect
clerm register
clerm search
clerm discover
clerm relationship establish
clerm relationship status
clerm token issue
clerm token refresh
clerm token inspect
clerm tools
clerm request
clerm invoke
clerm resolve
clerm serve
clerm benchmark
clerm shellenv
```

`resolve` remains a local decode/debug tool. `serve` now runs the local resolver daemon. Production authority flows for discovery, relationships, and token issuance still go through `clerm_registry`.

## Quick Start

Compile a schema:

```bash
mkdir -p schemas
clerm compile -in examples/provider_search.clermfile -out schemas/provider_search.clermcfg
clerm inspect -in schemas/provider_search.clermcfg
```

Register it in the registry:

```bash
clerm register \
  -registry http://127.0.0.1:8090 \
  -in schemas/provider_search.clermcfg \
  -owner seller-1
```

Discover it:

```bash
clerm search \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -query healthcare
```

Establish a protected relationship:

```bash
clerm relationship establish \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -schema schemas/provider_search.clermcfg \
  -relation @verified
```

Issue server-managed tokens:

```bash
clerm token issue \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -schema schemas/provider_search.clermcfg \
  -method @verified.healthcare.book_visit.v1 \
  -out-cap visit.token \
  -out-refresh visit.refresh
```

Build requests:

```bash
clerm request \
  -schema schemas/provider_search.clermcfg \
  -method @global.healthcare.search_providers.v1 \
  -allow @global \
  -data-file examples/search_providers.payload \
  -out search_providers.clerm

clerm request \
  -schema schemas/provider_search.clermcfg \
  -method @verified.healthcare.book_visit.v1 \
  -allow @verified \
  -data-file examples/book_visit.payload \
  -cap-file visit.token \
  -out book_visit.clerm
```

Invoke through the registry:

```bash
clerm invoke \
  -registry http://127.0.0.1:8090 \
  -schema schemas/provider_search.clermcfg \
  -request search_providers.clerm
```

Refresh an access token:

```bash
clerm token refresh \
  -registry http://127.0.0.1:8090 \
  -refresh-token "$(tr -d '\n' < visit.refresh)" \
  -out-cap visit.next.token \
  -out-refresh visit.next.refresh
```

## Testing

```bash
make vet
go test ./... -count=1
make test-race
make bench-resolver
make bench
make bench-split
make bench-escape
make bench-profile
```

For targeted microbenchmarks on the latest resolver, schema, and array-validation hot-path changes:

```bash
go test ./tests/bench/schema ./tests/bench/resolver ./tests/bench/clermwire -bench 'Benchmark(PublicFingerprint|CachedPublicFingerprint|InvocationArgumentsAccess|ValidateArrayEnvelope)' -benchmem -run '^$'
```

Resolver docs live in `docs/`.

- `docs/README.md`
- `docs/resolver-architecture.md`
- `docs/resolver-go.md`
- `docs/resolver-daemon.md`
- `docs/other-languages.md`
- `docs/resolver-testing.md`

A full setup and command walkthrough is in `examples/setup.md`.
