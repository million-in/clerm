# Embedded Go Resolver

## Build

```bash
make build
make build-resolver
```

## Load A Schema

```go
service, err := resolver.LoadConfig("schemas/provider_search.clermcfg")
if err != nil {
    log.Fatal(err)
}
```

Or load from a registry URL:

```go
service, err := resolver.LoadConfigURL(context.Background(), "https://registry.example/schema/provider_search.clermcfg", nil)
if err != nil {
    log.Fatal(err)
}
```

## Configure Capability Verification

```go
publicKey, err := capability.ReadPublicKeyFile("registry.ed25519.pub")
if err != nil {
    log.Fatal(err)
}
service.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{
    "registry": publicKey,
}))
```

## Bind Handlers

```go
err = service.Bind("@global.healthcare.search_providers.v1", func(ctx context.Context, invocation *resolver.Invocation) (*resolver.Result, error) {
    return resolver.Success(map[string]any{
        "request_id": "123e4567-e89b-12d3-a456-426614174000",
        "providers": []map[string]any{{"id": "provider-1"}},
    }), nil
})
```

For low-allocation hot paths, prebuild the CLERM response once and reuse it:

```go
response, err := resolver.BuildSuccessResponse(method, map[string]any{
    "request_id": "123e4567-e89b-12d3-a456-426614174000",
    "providers": []map[string]any{{"id": "provider-1"}},
})
if err != nil {
    log.Fatal(err)
}

err = service.Bind("@global.healthcare.search_providers.v1", func(ctx context.Context, invocation *resolver.Invocation) (*resolver.Result, error) {
    return resolver.SuccessResponse(response), nil
})
```

## Attach To An Existing Route

```go
mux := http.NewServeMux()
mux.Handle("/graphql", service.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusNotFound)
})))
```

The resolver only intercepts requests with `Content-Type: application/clerm`.

## Method Handler Contract

Each bound handler receives:

- schema name
- schema fingerprint
- method metadata
- exact target
- decoded request arguments
- capability metadata when present

Each handler returns either:

- `resolver.Success(outputs)`
- `resolver.SuccessResponse(response)`
- `resolver.Failure(code, message)`
- a normal Go error

Go errors become CLERM error responses after method resolution.
