# Resolver Testing

## Build

```bash
make build
make build-resolver
```

## Run Tests

```bash
make vet
go test ./... -count=1
make test-race
make test-unit
make test-integration
make test-e2e
```

## Run Resolver Benchmarks

```bash
make bench-resolver
```

To compare the schema fingerprint cache, the read-only invocation argument
view, and the ARRAY envelope validation path directly:

```bash
go test ./tests/bench/schema ./tests/bench/resolver ./tests/bench/clermwire -bench 'Benchmark(PublicFingerprint|CachedPublicFingerprint|InvocationArgumentsAccess|ValidateArrayEnvelope)' -benchmem -run '^$'
```

## Run All Benchmarks

```bash
make bench
```

## Manual Local Flow

```bash
clerm compile -in examples/provider_search.clermfile -out schemas/provider_search.clermcfg
bin/clerm-resolver -schema schemas/provider_search.clermcfg -cap-public-key registry.ed25519.pub -listen 127.0.0.1:8181
```

Or run it over a Unix socket:

```bash
bin/clerm-resolver -schema schemas/provider_search.clermcfg -cap-public-key registry.ed25519.pub -unix-socket /tmp/clerm-resolver.sock
```

Build a request:

```bash
clerm request \
  -schema schemas/provider_search.clermcfg \
  -method @global.healthcare.search_providers.v1 \
  -allow @global \
  -data-file examples/search_providers.payload \
  -out search_providers.clerm
```

Decode through the daemon:

```bash
curl \
  -X POST http://127.0.0.1:8181/v1/requests/decode \
  -H 'Content-Type: application/clerm' \
  -H 'Clerm-Target: internal.search' \
  --data-binary @search_providers.clerm
```
