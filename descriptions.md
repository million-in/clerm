Good. Everything is coherent now. Here is your stable draft spec.

---

# CLERM Protocol Specification — v0.1 (Stable Draft)

## What CLERM Is

CLERM is a **schema-gated communication protocol for autonomous agents**. It sits in front of existing APIs and enforces structured, validated, capability-controlled access. Agents never call internal services directly. They interact only with declared schemas. Resolvers execute the internal logic.

CLERM is not an API framework. It is a **secure gateway protocol**.

---

## Core Guarantee

An agent cannot access more than what its capability token and the target schema explicitly permit. Prompt injection is neutralized at the protocol layer, not the application layer.

---

## Transport

- HTTP/2 and HTTP/3 over TLS
- Resolver activation detected via **content-type header** — if payload is `.clerm`, the resolver intercepts. Otherwise the request continues through normal API routing.

---

## Wire Format

Requests are transmitted as **`.clerm` binaries** — a custom, schema-based binary format built from scratch. Design goals:

- Schema-aware (not schemaless like MessagePack)
- Near-zero deserialization cost at the resolver
- Simple wire layout, no external format dependency
- Deterministic decoding

Source format is `.clermfile` — human-readable DSL with comment support, compiled to `.clerm` for transmission.

---

## Request Identity — Capability Token

Every CLERM request carries a **Capability Token** (not a Request ID in the traditional sense).

The token encodes:

| Field | Purpose |
|---|---|
| Platform identifier | Which platform is making the request |
| Schema identifier | Which schema is being accessed |
| Relationship proof | Encoded proof of established agent-to-service relationship |
| Nonce | Single-use value, prevents replay |
| Timestamp | Short TTL window, renders captured tokens obsolete |
| Sequence number *(v2)* | Ordered request tracking, added in later version |

Resolvers reject tokens that fail nonce validation, fall outside timestamp window, or carry unrecognized relationship proofs.

---

## Relationship Model

Relationships are **agent-to-service**, defined per schema endpoint by the service owner.

Each schema endpoint declares its relationship requirements:

- What kind of agent may call it
- Whether the relationship is self-established or requires human or system approval to elevate
- Relationship state is managed server-side via an internal CLERM relationship endpoint

Access tiers are **hybrid capability + RBAC**:

| Tier | Description |
|---|---|
| Public | No relationship required |
| Authenticated Consumer | Agent acting on behalf of a user, relationship established |
| Partner | Agent representing a partner service, requires explicit approval |

Elevation from one tier to another is managed through the internal relationship system, not inside the schema itself.

---

## Resolver Gateway

Resolvers are gateways between incoming CLERM requests and internal APIs.

Resolver responsibilities in order:

1. Detect `.clerm` content-type
2. Decode binary payload
3. Validate schema match
4. Verify capability token (nonce, timestamp, relationship proof)
5. Execute internal resolver logic
6. Return structured response

Resolvers attach to existing services. They do not create new API endpoints.

---

## Schema Access Control

Schemas declare what is accessible and to whom. Resolvers declare how it is retrieved.

```clerm
schema bookstore.purchase_book {
    description: "Purchase a book on behalf of a user"
    access: authenticated_consumer
    relationship: required
    input {
        book_id: string
        user_token: string
    }
    output {
        order_id: string
        status: string
    }
    execution: sync
}
```

---

## Execution Modes

| Mode | Behavior |
|---|---|
| `sync` | Agent blocks and waits for response |
| `async` | Agent submits request, polls for result via a CLERM polling schema |

Polling is protocol-native — the async result endpoint is itself a CLERM schema, not a raw HTTP callback.

---

## Schema Registry and Discovery

**Global centralized registry** — one authoritative source, not open-source, not per-deployment.

Responsibilities:

- Schema publication and versioning
- Legacy schema maintenance
- Version negotiation between agents and resolvers

Discovery query:

```
discover bookstore.schemas
```

Response:

```
bookstore.search_books.v1
bookstore.search_books.v2
bookstore.purchase_book.v1
```

Old agents continue functioning on legacy schema versions. New versions coexist.

---

## Schema Evolution

Versioning is first-class:

```
bookstore.search_books.v1   ← legacy, maintained
bookstore.search_books.v2   ← current
```

Resolvers may support multiple versions simultaneously. Deprecation is registry-controlled.

---

## Security Model Summary

| Threat | Mitigation |
|---|---|
| Prompt injection | Schema validation — agents cannot escape declared schema bounds |
| Replay attack | Nonce + short-TTL timestamp on every capability token |
| Unauthorized access | Capability token encodes relationship proof, verified by resolver |
| Internal API exposure | Resolvers only — no internal routes are ever externally reachable |

---

## Implementation Language

Core infrastructure: **Go**

Reasons: concurrency model, binary handling efficiency, networking performance, long-running service stability.

---

## What Is Explicitly Out of Scope for v0.1

- Sequence numbers on tokens (v2)
- Federated resolver networks
- Schema marketplace or paid schema access
- Recommendations engine on discovery

---

## Build Order (Roadmap)

| Phase | Deliverable |
|---|---|
| 1 | `.clermfile` DSL parser and `.clerm` binary compiler |
| 2 | Capability token structure — encode, decode, validate |
| 3 | Resolver gateway — content-type detection, schema validation, token verification |
| 4 | Relationship management system — internal endpoint, tier elevation |
| 5 | Async polling schema — native CLERM polling protocol |
| 6 | Global schema registry — publish, version, discover |
| 7 | SDK — schema tools, resolver framework, request client |

---

