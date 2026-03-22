# CLERM Setup

## 1. Build the compiler

```bash
make build
eval "$(bin/clerm shellenv)"
clerm help
```

## 2. Write a new schema

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

## 3. Compile and inspect

```bash
mkdir -p schemas
clerm compile -in clinic_gateway.clermfile -out schemas/clinic_gateway.clermcfg
clerm inspect -in schemas/clinic_gateway.clermcfg
clerm inspect -in schemas/clinic_gateway.clermcfg -internal
```

## 4. Generate tool-call schemas

```bash
clerm tools -schema schemas/clinic_gateway.clermcfg -allow @global
clerm tools -schema schemas/clinic_gateway.clermcfg -allow @verified
```

## 5. Create payloads

```bash
cat > search_providers.payload <<'EOF_PAYLOAD'
{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}
EOF_PAYLOAD

cat > book_visit.payload <<'EOF_PAYLOAD'
{"provider_id":"abc123","user_token":"tok_123"}
EOF_PAYLOAD
```

## 6. Generate keys and a verified token

```bash
clerm token keygen -out-private dev.ed25519 -out-public dev.ed25519.pub
clerm token issue \
  -schema schemas/clinic_gateway.clermcfg \
  -method @verified.clinic.book_visit.v1 \
  -issuer registry \
  -subject partner-123 \
  -private-key dev.ed25519 \
  -out book_visit.token
clerm token inspect -in book_visit.token
```

## 7. Build `.clerm` requests

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

## 8. Inspect and resolve

```bash
clerm inspect -in search_providers.clerm
clerm inspect -in book_visit.clerm

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

## 9. Run the local resolver

```bash
clerm serve \
  -schema schemas/clinic_gateway.clermcfg \
  -listen 127.0.0.1:8080 \
  -cap-public-key dev.ed25519.pub
```

## 10. Send a request to the resolver

```bash
curl \
  -X POST http://127.0.0.1:8080/resolve \
  -H 'Content-Type: application/clerm' \
  -H 'Clerm-Target: registry.discover' \
  --data-binary @search_providers.clerm
```

## 11. Run tests and benchmarks

```bash
make test
make bench
make bench-split
make bench-escape
make bench-profile
```
