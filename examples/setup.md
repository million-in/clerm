# CLERM Setup

## 1. Build the compiler

```bash
make build
eval "$(bin/clerm shellenv)"
clerm help
```

## 2. Start the registry

The public CLI expects a running `clerm_registry` RPC server. The default URL is `http://127.0.0.1:8090`.

## 3. Compile a schema

```bash
mkdir -p schemas
clerm compile -in examples/provider_search.clermfile -out schemas/provider_search.clermcfg
clerm inspect -in schemas/provider_search.clermcfg
clerm inspect -in schemas/provider_search.clermcfg -internal
```

## 4. Register the compiled schema

```bash
clerm register \
  -registry http://127.0.0.1:8090 \
  -in schemas/provider_search.clermcfg \
  -owner seller-1
```

## 5. Search and discover

```bash
clerm search \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -query healthcare

clerm discover \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -query booking
```

## 6. Manage relationships

```bash
clerm relationship status \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -schema schemas/provider_search.clermcfg

clerm relationship establish \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -schema schemas/provider_search.clermcfg \
  -relation @verified
```

## 7. Issue server-managed tokens

```bash
clerm token issue \
  -registry http://127.0.0.1:8090 \
  -consumer buyer-1 \
  -schema schemas/provider_search.clermcfg \
  -method @verified.healthcare.book_visit.v1 \
  -out-cap visit.token \
  -out-refresh visit.refresh

clerm token inspect -in visit.token
```

Refresh them:

```bash
clerm token refresh \
  -registry http://127.0.0.1:8090 \
  -refresh-token "$(tr -d '\n' < visit.refresh)" \
  -out-cap visit.next.token \
  -out-refresh visit.next.refresh
```

## 8. Build `.clerm` requests

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

## 9. Invoke through the registry

```bash
clerm invoke \
  -registry http://127.0.0.1:8090 \
  -schema schemas/provider_search.clermcfg \
  -request search_providers.clerm
```

If you pass `-out response.body`, the raw upstream body is written to disk and the CLI prints a JSON envelope with status and headers.

## 10. Debug locally without the registry

These are debug tools only:

```bash
clerm resolve \
  -schema schemas/provider_search.clermcfg \
  -request search_providers.clerm \
  -target registry.discover

clerm serve \
  -schema schemas/provider_search.clermcfg \
  -listen 127.0.0.1:8080
```

## 11. Run tests and benchmarks

```bash
go test ./... -count=1
make bench
make bench-split
make bench-escape
make bench-profile
```
