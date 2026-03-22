# Resolver Architecture

The resolver is a CLERM runtime, not a standalone public product endpoint.

## Goals

- decode `application/clerm` requests
- verify capability tokens issued by `clerm_registry`
- validate requests against a compiled `.clermcfg`
- dispatch to application handlers
- encode CLERM responses back to the caller
- let host APIs keep their own routes

## Runtime Modes

### Embedded Go

A Go API loads a `.clermcfg`, binds handlers by exact method reference, and wraps an existing route with resolver middleware.

The resolver only intercepts requests whose `Content-Type` is `application/clerm`. All other traffic falls through unchanged.

### Local Daemon

`clerm-resolver` runs as a local HTTP daemon for non-Go projects.

It exposes local-only helper endpoints:

- `GET /healthz`
- `GET /v1/schema`
- `POST /v1/requests/decode`
- `POST /v1/responses/encode`

A non-Go service receives a CLERM payload on its normal application route, sends that payload to the daemon for decode/verification, runs its own local handler logic, then asks the daemon to encode the response back into CLERM bytes.

## Request Flow

1. host API receives `Content-Type: application/clerm`
2. resolver decodes the `.clerm` payload
3. resolver validates the request against the loaded `.clermcfg`
4. resolver verifies the embedded capability token when the relation requires it
5. resolver resolves the method binding
6. application handler runs
7. resolver validates handler output against the method output schema
8. resolver encodes a CLERM response frame

## Response Frames

CLERM responses are binary frames with method identity and ordered typed outputs.

They can also carry a structured CLERM error body.

Protocol-level failures before method resolution still return normal HTTP errors because there is no safe method context to build a CLERM response.

## Schema Sources

The resolver can load a schema from:

- a local `.clermcfg` file
- a remote `.clermcfg` URL returned by the registry

Schema loading happens before the request hot path.

## Capability Verification

Resolver verification uses the registry public key.

The verifier checks:

- signature
- time window
- schema name
- schema fingerprint
- relation condition
- method scope
- target scope
- replay store reservation

## Performance Rules

The hot path avoids:

- schema parsing per request
- route lookup by scanning
- remote schema fetch on request path
- dynamic handler registration locks on read path

The runtime uses:

- immutable method table
- copy-on-write handler bindings
- compact request decode
- compact response encode
- content-type based interception instead of path-based routing
