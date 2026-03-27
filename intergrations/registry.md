# CLERM Registry Integration Guide

This document defines the public HTTP contract exposed by the registry.

It is intended for client and provider implementers who need to integrate with the API without reading the registry source code.

This document covers:

- endpoint paths and HTTP methods
- required headers
- request body formats
- response body formats
- error behavior
- the expected end-to-end integration flow

This document does not cover internal storage, cache, or indexing implementation details.

## Base URL

All examples assume a base URL such as:

```text
https://registry.example.com
```

## API Overview

The registry exposes:

- `GET /healthz`
- `POST /rpc`

All RPC operations use the same path, `POST /rpc`, and select the operation through the `Clerm-Target` header.

Supported `Clerm-Target` values:

- `registry.register`
- `registry.search`
- `registry.discover`
- `registry.relationship.establish`
- `registry.relationship.status`
- `registry.token.issue`
- `registry.token.refresh`
- `registry.invoke`

## General Conventions

### Request ID

Every response includes:

```text
X-Request-Id: <opaque-request-id>
```

Use this for debugging and support.

### Error Responses

Errors are returned as plain text, not JSON.

Status code mapping:

- `400 Bad Request`
- `404 Not Found`
- `500 Internal Server Error`

Public error bodies are intentionally generic:

- `invalid_argument: bad request`
- `parse_error: malformed request`
- `validation_error: request validation failed`
- `not_found: resource not found`
- `io_error: request failed`
- `internal_error: internal server error`

Do not build client logic around internal error wording beyond these public strings and status codes.

### Time Format

All timestamps in JSON responses use RFC 3339 format.

Example:

```text
2026-03-27T18:25:11Z
```

### Signed Schema URLs

When a response includes `schema_url`, it is a signed Hetzner `GET` URL for the stored `.clermcfg` file.

Important:

- it is temporary and expires
- it should be used with `GET`
- clients should treat it as ephemeral and re-request fresh API data when needed
- the registry does not currently return a separate expiry field for `schema_url`

## Shared Response Objects

### Schema Summary

Returned by register, search, and discover.

```json
{
  "fingerprint": "string",
  "public_fingerprint": "string",
  "schema_name": "string",
  "owner_id": "string",
  "status": "active|disabled|archived",
  "schema_url": "string",
  "metadata": {
    "display_name": "string",
    "description": "string",
    "category": "string",
    "tags": ["string"]
  },
  "methods": [
    {
      "reference": "string",
      "relation": "string",
      "condition": "string",
      "execution": "string",
      "input_count": 0,
      "output_count": 0,
      "output_format": "string"
    }
  ],
  "relations": [
    {
      "name": "string",
      "condition": "string",
      "status": "string",
      "token_required": true
    }
  ]
}
```

Notes:

- `schema_url` is the signed Hetzner download URL for the stored compiled schema file
- `relations[].status` is populated when relationship data exists for the requesting consumer
- `methods[].reference` is the exact method string clients must use for token issuance

### Relationship Object

```json
{
  "consumer_id": "string",
  "provider_fingerprint": "string",
  "relation": "string",
  "status": "active|pending|revoked",
  "created_at": "RFC3339 timestamp",
  "updated_at": "RFC3339 timestamp"
}
```

### Token Response

Returned by token issue and token refresh.

```json
{
  "capability_token": "string",
  "expires_at": "RFC3339 timestamp",
  "refresh_token": "string",
  "refresh_expires_at": "RFC3339 timestamp",
  "relation": "string",
  "condition": "string"
}
```

## Health Check

### `GET /healthz`

Checks whether the service process is up.

Example:

```bash
curl -i https://registry.example.com/healthz
```

Success response:

- status: `200 OK`
- content type: `text/plain; charset=utf-8`
- body:

```text
ok
```

## RPC Operations

All RPC operations use:

```text
POST /rpc
```

and require:

```text
Clerm-Target: <operation-name>
```

### 1. Register Schema

### `Clerm-Target: registry.register`

Registers a compiled `.clermcfg` provider schema.

Canonical request format:

- method: `POST`
- path: `/rpc`
- content type: `application/clermcfg`
- body: raw compiled `.clermcfg` bytes

Required headers for binary registration:

- `Clerm-Target: registry.register`
- `Clerm-Owner: <owner-id>`

Optional headers for binary registration:

- `Clerm-Status: active|disabled|archived`

If `Clerm-Status` is omitted, it defaults to `active`.

Binary example:

```bash
curl -sS -X POST https://registry.example.com/rpc \
  -H 'Content-Type: application/clermcfg' \
  -H 'Clerm-Target: registry.register' \
  -H 'Clerm-Owner: seller-1' \
  -H 'Clerm-Status: active' \
  --data-binary @provider_search.clermcfg
```

Compatibility JSON request format:

- content type: `application/json`
- body:

```json
{
  "owner_id": "seller-1",
  "status": "active",
  "clermcfg_base64": "<raw-std-base64-without-padding>"
}
```

Success response:

```json
{
  "registration_status": "registered",
  "schema": {
    "fingerprint": "string",
    "public_fingerprint": "string",
    "schema_name": "string",
    "owner_id": "string",
    "status": "active",
    "schema_url": "https://...signed-hetzner-get-url...",
    "metadata": {
      "display_name": "string",
      "description": "string",
      "category": "string",
      "tags": ["string"]
    },
    "methods": [
      {
        "reference": "string",
        "relation": "string",
        "condition": "string",
        "execution": "string",
        "input_count": 0,
        "output_count": 0,
        "output_format": "string"
      }
    ],
    "relations": [
      {
        "name": "string",
        "condition": "string",
        "token_required": true
      }
    ]
  }
}
```

Client requirements:

- keep `schema.fingerprint`; it is the provider identity used in later API calls
- keep `schema.methods[].reference`; token issuance must use one of these exact strings
- treat `schema.schema_url` as temporary

### 2. Search

### `Clerm-Target: registry.search`

Searches active schemas using text and optional filters.

Request:

```json
{
  "consumer_id": "buyer-1",
  "query": "provider search",
  "relations": ["@verified"],
  "categories": ["healthcare"],
  "tags": ["booking"],
  "limit": 20,
  "offset": 0
}
```

Fields:

- `consumer_id`: optional but recommended; used to attach relationship status to returned relations
- `query`: optional free text
- `relations`: optional list of relation names
- `categories`: optional list of categories
- `tags`: optional list of tags
- `limit`: optional, defaults to `20`, maximum `100`
- `offset`: optional, defaults to `0`

Success response:

```json
{
  "results": [
    {
      "fingerprint": "string",
      "public_fingerprint": "string",
      "schema_name": "string",
      "owner_id": "string",
      "status": "active",
      "schema_url": "https://...signed-hetzner-get-url...",
      "metadata": {
        "display_name": "string",
        "description": "string",
        "category": "string",
        "tags": ["string"]
      },
      "methods": [],
      "relations": []
    }
  ]
}
```

If nothing matches:

```json
{
  "results": []
}
```

### 3. Discover

### `Clerm-Target: registry.discover`

`registry.discover` currently uses the same request and response contract as `registry.search`.

Request:

```json
{
  "consumer_id": "buyer-1",
  "query": "provider search",
  "relations": ["@verified"],
  "categories": ["healthcare"],
  "tags": ["booking"],
  "limit": 20,
  "offset": 0
}
```

Response:

```json
{
  "results": []
}
```

### 4. Establish Or Update Relationship

### `Clerm-Target: registry.relationship.establish`

Creates or updates the relationship status between a consumer and a provider for a specific relation.

Request:

```json
{
  "consumer_id": "buyer-1",
  "provider_fingerprint": "provider-fingerprint",
  "relation": "@verified",
  "status": "active"
}
```

Fields:

- `consumer_id`: required
- `provider_fingerprint`: required
- `relation`: required and must exist in the provider schema
- `status`: optional, defaults to `active`

Allowed relationship statuses:

- `active`
- `pending`
- `revoked`

Success response:

```json
{
  "relationship": {
    "consumer_id": "buyer-1",
    "provider_fingerprint": "provider-fingerprint",
    "relation": "@verified",
    "status": "active",
    "created_at": "2026-03-27T18:25:11Z",
    "updated_at": "2026-03-27T18:25:11Z"
  }
}
```

Behavior:

- calling this endpoint again for the same consumer, provider, and relation updates `status` and `updated_at`

### 5. Relationship Status

### `Clerm-Target: registry.relationship.status`

Lists all known relationships for a consumer against a single provider fingerprint.

Request:

```json
{
  "consumer_id": "buyer-1",
  "provider_fingerprint": "provider-fingerprint"
}
```

Success response:

```json
{
  "relationships": [
    {
      "consumer_id": "buyer-1",
      "provider_fingerprint": "provider-fingerprint",
      "relation": "@verified",
      "status": "active",
      "created_at": "2026-03-27T18:25:11Z",
      "updated_at": "2026-03-27T18:25:11Z"
    }
  ]
}
```

If no relationship exists, the response is still `200 OK`:

```json
{
  "relationships": []
}
```

### 6. Issue Capability Token

### `Clerm-Target: registry.token.issue`

Issues an invoke capability token and a refresh token.

Request:

```json
{
  "consumer_id": "buyer-1",
  "provider_fingerprint": "provider-fingerprint",
  "method": "@verified.healthcare.book_visit.v1",
  "subject": "buyer-1",
  "targets": ["registry.invoke"],
  "invoke_ttl_seconds": 1800,
  "refresh_ttl_seconds": 604800
}
```

Fields:

- `consumer_id`: required in normal integrations
- `provider_fingerprint`: required
- `method`: optional if `relation` is provided
- `relation`: optional if `method` is provided
- `subject`: optional, defaults to `consumer_id`
- `targets`: optional, defaults to `["registry.invoke"]`
- `invoke_ttl_seconds`: optional
- `refresh_ttl_seconds`: optional and must be greater than invoke TTL

Rules:

- either `method` or `relation` must be provided
- if `method` is provided, it must exactly match one of `schema.methods[].reference`
- if the selected relation requires a relationship, that relationship must already exist and be `active`

Success response:

```json
{
  "capability_token": "string",
  "expires_at": "2026-03-27T18:55:11Z",
  "refresh_token": "string",
  "refresh_expires_at": "2026-04-03T18:25:11Z",
  "relation": "@verified",
  "condition": "auth.required"
}
```

Integration notes:

- clients must embed `capability_token` into the compiled `.clerm` invoke request
- token issuance does not perform the invoke itself

### 7. Refresh Capability Token

### `Clerm-Target: registry.token.refresh`

Rotates a refresh session and returns a new capability token plus a new refresh token.

Request:

```json
{
  "refresh_token": "string",
  "targets": ["registry.invoke"],
  "invoke_ttl_seconds": 1800,
  "refresh_ttl_seconds": 604800
}
```

Fields:

- `refresh_token`: required
- `targets`: optional; if omitted, the previous session targets are reused
- `invoke_ttl_seconds`: optional
- `refresh_ttl_seconds`: optional and must be greater than invoke TTL

Success response:

```json
{
  "capability_token": "string",
  "expires_at": "2026-03-27T19:25:11Z",
  "refresh_token": "string",
  "refresh_expires_at": "2026-04-03T18:55:11Z",
  "relation": "@verified",
  "condition": "auth.required"
}
```

Integration notes:

- the old refresh token should be treated as replaced
- clients should update both tokens atomically on success

### 8. Invoke

### `Clerm-Target: registry.invoke`

Forwards a compiled `.clerm` request to the provider route described by the registered schema.

Request format:

- method: `POST`
- path: `/rpc`
- content type: `application/clerm`
- body: raw compiled `.clerm` bytes with embedded capability token

Required headers:

- `Clerm-Target: registry.invoke`
- `Clerm-Schema-Fingerprint: <provider-fingerprint>`

Example:

```bash
curl -i -X POST https://registry.example.com/rpc \
  -H 'Content-Type: application/clerm' \
  -H 'Clerm-Target: registry.invoke' \
  -H 'Clerm-Schema-Fingerprint: provider-fingerprint' \
  --data-binary @request.clerm
```

Requirements:

- the `.clerm` body must be valid compiled CLERM request bytes
- the embedded capability token must be valid for the selected provider schema
- the embedded method must match the granted capability scope
- the provider fingerprint header must identify the schema used to validate and route the request

Success behavior:

- the registry forwards the request upstream
- the registry returns the upstream HTTP status code
- the registry returns the upstream response body
- the response includes:
  - `Clerm-Target: registry.invoke`
  - `Clerm-Command-Method: <resolved-method-reference>`

Important response behavior:

- safe upstream headers may be forwarded
- hop-by-hop headers are removed
- `Set-Cookie`, `Server`, and internal CLERM routing headers are not forwarded

Example successful response headers:

```text
HTTP/1.1 200 OK
Clerm-Target: registry.invoke
Clerm-Command-Method: @verified.healthcare.book_visit.v1
Content-Type: application/json
X-Request-Id: <request-id>
```

## Recommended Integration Flow

For providers:

1. compile a provider schema to `.clermcfg`
2. call `registry.register`
3. store `schema.fingerprint`
4. optionally verify `schema.schema_url` by downloading the compiled schema bytes

For consumers:

1. call `registry.search` or `registry.discover`
2. choose a provider fingerprint and method reference
3. establish the required relationship if the chosen relation requires one
4. call `registry.token.issue`
5. compile the invoke request to `.clerm`, embedding the returned capability token
6. call `registry.invoke`
7. before capability expiry, optionally rotate with `registry.token.refresh`

## Practical Client Rules

- never hardcode a method string unless it comes from the provider schema you are actually using
- treat `schema_url` as temporary
- treat `refresh_token` as a secret
- replace refresh tokens after a successful refresh response
- use `X-Request-Id` when reporting issues
- do not assume search and discover will return the provider you just registered unless your query actually matches its metadata

## Minimal End-To-End Example

Register:

```bash
curl -sS -X POST https://registry.example.com/rpc \
  -H 'Content-Type: application/clermcfg' \
  -H 'Clerm-Target: registry.register' \
  -H 'Clerm-Owner: seller-1' \
  -H 'Clerm-Status: active' \
  --data-binary @provider_search.clermcfg
```

Search:

```bash
curl -sS -X POST https://registry.example.com/rpc \
  -H 'Content-Type: application/json' \
  -H 'Clerm-Target: registry.search' \
  -d '{"consumer_id":"buyer-1","query":"provider search"}'
```

Establish relationship:

```bash
curl -sS -X POST https://registry.example.com/rpc \
  -H 'Content-Type: application/json' \
  -H 'Clerm-Target: registry.relationship.establish' \
  -d '{"consumer_id":"buyer-1","provider_fingerprint":"<fingerprint>","relation":"@verified","status":"active"}'
```

Issue token:

```bash
curl -sS -X POST https://registry.example.com/rpc \
  -H 'Content-Type: application/json' \
  -H 'Clerm-Target: registry.token.issue' \
  -d '{"consumer_id":"buyer-1","provider_fingerprint":"<fingerprint>","method":"<exact-method-reference>"}'
```

Invoke:

```bash
curl -i -X POST https://registry.example.com/rpc \
  -H 'Content-Type: application/clerm' \
  -H 'Clerm-Target: registry.invoke' \
  -H 'Clerm-Schema-Fingerprint: <fingerprint>' \
  --data-binary @request.clerm
```
