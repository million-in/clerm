# CLERM

CLERM is the public compiler and wire-format toolchain for schema-defined request execution.

It produces three artifacts:

- `.clermfile`: human-written schema source
- `.clermcfg`: compiled schema configuration payload
- `.clerm`: compiled single-method request payload

`.clerm` and `.clermcfg` are not executables. They are compact binary wire payloads that can be decoded by CLERM-aware services.

## Scope

This repository contains:

- schema parsing and validation
- `.clermcfg` compilation and inspection
- `.clerm` request generation and inspection
- capability token generation and verification
- offline resolve and local resolver decode
- benchmarks for binary vs JSON encoding and decoding

The registry is a separate private service. It stores `.clermcfg`, indexes discovery metadata, manages relationships, issues invoke tokens, and routes requests using internal schema routes.

## Schema Metadata

A schema can define public registry metadata under `@metadata:`:

- `description`
- `tags`
- `display_name`
- `category`

That metadata is compiled into `.clermcfg` for registry storage and search.

## Build

```bash
make build
eval "$(bin/clerm shellenv)"
clerm help
```

`shellenv` exports `./bin` into `PATH`, so the compiled binary can be used as `clerm` instead of `./bin/clerm`.

## Commands

```bash
clerm help
clerm compile
clerm inspect
clerm tools
clerm token keygen
clerm token issue
clerm token inspect
clerm request
clerm resolve
clerm serve
clerm benchmark
clerm shellenv
```

## Quick Start

Write a schema:

```bash
cat > clinic_gateway.clermfile <<'EOF_SCHEMA'
schema @general.avail.mandene
  @metadata:
    description: Clinic search and booking schema
    tags: healthcare, booking, discovery
    display_name: Clinic Gateway
    category: healthcare
  @route: https://clinic.internal.example/clerm
  service: @global.clinic.search_providers.v1
  service: @verified.clinic.book_visit.v1

method @global.clinic.search_providers.v1
  @exec: async.pool
  @args_input: 3
    decl_args: specialty.STRING, latitude.DECIMAL, longitude.DECIMAL
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

method @verified.clinic.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: booking_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @global: any.protected
  @verified: auth.required
EOF_SCHEMA
```

Compile and inspect:

```bash
mkdir -p schemas
clerm compile -in clinic_gateway.clermfile -out schemas/clinic_gateway.clermcfg
clerm inspect -in schemas/clinic_gateway.clermcfg
clerm inspect -in schemas/clinic_gateway.clermcfg -internal
```

Generate callable tool definitions:

```bash
clerm tools -schema schemas/clinic_gateway.clermcfg -allow @global
clerm tools -schema schemas/clinic_gateway.clermcfg -allow @verified
```

Create payloads:

```bash
cat > search_providers.payload <<'EOF_PAYLOAD'
{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}
EOF_PAYLOAD

cat > book_visit.payload <<'EOF_PAYLOAD'
{"provider_id":"abc123","user_token":"tok_123"}
EOF_PAYLOAD
```

Create keys and issue a token:

```bash
clerm token keygen -out-private dev.ed25519 -out-public dev.ed25519.pub
clerm token issue \
  -schema schemas/clinic_gateway.clermcfg \
  -method @verified.clinic.book_visit.v1 \
  -issuer registry \
  -subject partner-123 \
  -private-key dev.ed25519 \
  -out book_visit.token
```

Build requests:

```bash
clerm request \
  -schema schemas/clinic_gateway.clermcfg \
  -method @global.clinic.search_providers.v1 \
  -allow @global \
  -data-file search_providers.payload \
  -out search_providers.clerm

clerm request \
  -schema schemas/clinic_gateway.clermcfg \
  -method @verified.clinic.book_visit.v1 \
  -allow @verified \
  -data-file book_visit.payload \
  -cap-file book_visit.token \
  -out book_visit.clerm
```

Resolve offline:

```bash
clerm resolve \
  -schema schemas/clinic_gateway.clermcfg \
  -request search_providers.clerm \
  -target registry.discover

clerm resolve \
  -schema schemas/clinic_gateway.clermcfg \
  -request book_visit.clerm \
  -target registry.invoke \
  -cap-public-key dev.ed25519.pub
```

Run the local resolver:

```bash
clerm serve \
  -schema schemas/clinic_gateway.clermcfg \
  -listen 127.0.0.1:8080 \
  -cap-public-key dev.ed25519.pub
```

Send a `.clerm` payload:

```bash
curl \
  -X POST http://127.0.0.1:8080/resolve \
  -H 'Content-Type: application/clerm' \
  -H 'Clerm-Target: registry.discover' \
  --data-binary @search_providers.clerm
```

## Testing

```bash
make test
make bench
make bench-split
make bench-escape
make bench-profile
```

A complete reproduction flow is in `examples/setup.md`.
