# Local Resolver Daemon

## Purpose

`clerm-resolver` is the bridge for non-Go projects before official CLERM SDKs exist.

It loads a schema, verifies capability tokens, and exposes a local helper API for decode and response encoding.

## Build

```bash
make build-resolver
```

## Run From A Local File

```bash
bin/clerm-resolver \
  -schema schemas/provider_search.clermcfg \
  -cap-public-key registry.ed25519.pub \
  -listen 127.0.0.1:8181
```

## Run From A Registry URL

```bash
bin/clerm-resolver \
  -schema-url https://registry.example/schema/provider_search.clermcfg \
  -cap-public-key registry.ed25519.pub \
  -listen 127.0.0.1:8181
```

## Run On A Unix Socket

```bash
bin/clerm-resolver \
  -schema schemas/provider_search.clermcfg \
  -cap-public-key registry.ed25519.pub \
  -unix-socket /tmp/clerm-resolver.sock
```

Use the socket from local SDK shims or language adapters that should not bind a TCP port.

## Endpoints

### Health

```bash
curl http://127.0.0.1:8181/healthz
```

### Schema Summary

```bash
curl http://127.0.0.1:8181/v1/schema
```

### Decode A Request

```bash
curl \
  -X POST http://127.0.0.1:8181/v1/requests/decode \
  -H 'Content-Type: application/clerm' \
  -H 'Clerm-Target: internal.search' \
  --data-binary @search_providers.clerm
```

### Encode A Success Response

```bash
curl \
  -X POST http://127.0.0.1:8181/v1/responses/encode \
  -H 'Content-Type: application/json' \
  -d '{
    "method":"@global.healthcare.search_providers.v1",
    "outputs":{
      "request_id":"123e4567-e89b-12d3-a456-426614174000",
      "providers":[]
    }
  }' \
  --output search_providers_response.clerm
```

### Encode An Error Response

```bash
curl \
  -X POST http://127.0.0.1:8181/v1/responses/encode \
  -H 'Content-Type: application/json' \
  -d '{
    "method":"@verified.healthcare.book_visit.v1",
    "error":{
      "code":"validation_error",
      "message":"payment authorization failed"
    }
  }' \
  --output book_visit_error.clerm
```
