# Optimization And Security Review

Audit date: 2026-03-27

Scope reviewed sequentially:
- public library entrypoints: `clerm.go`, `compiler.go`, `compiler_vendor.go`, `resolver_api.go`, `resolver_forward.go`
- schema/config/request/response packages: `schema/`, `clermcfg/`, `clermreq/`, `clermresp/`
- internal runtime: `internal/schema/`, `internal/clermwire/`, `internal/resolver/`, `internal/capability/`, `internal/netutil/`
- CLI entrypoints: `internal/app/clermcli/`, `internal/app/resolvercli/`
- registry client: `registryrpc/`

Files without actionable production findings were still reviewed, but this document records only the files with confirmed bottlenecks or security risks plus the exact remediation applied.

## Confirmed findings and fixes

### `internal/app/clermcli/cli.go`

Status: fixed

Finding:
- `clerm serve` bound on all interfaces by default and had no HTTP timeouts.
- `clerm request` wrote `.clerm` payloads with world-readable permissions.

Relevant snippet before fix:

```go
listen := fs.String("listen", ":8080", "listen address")
server := &http.Server{Addr: *listen, Handler: clerm.Resolver.NewDaemonHandler(logger, service)}
if err := os.WriteFile(*out, encoded.Payload, 0o644); err != nil {
```

Impact:
- local debugging daemon was easy to expose accidentally
- slowloris-style connection pressure could hold sockets and goroutines
- capability-bearing request payloads could leak through file permissions

Remediation:
- default bind address changed to `127.0.0.1:8181`
- `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` added
- request output mode tightened to `0600`
- resolver service is now closed on daemon shutdown

### `internal/capability/replay.go`

Status: fixed

Finding:
- every default replay store started an endless cleanup goroutine without a shutdown path

Relevant snippet before fix:

```go
if sweepInterval > 0 {
    go store.cleanupLoop()
}
```

Impact:
- repeated service construction in library mode could leak background goroutines
- long-lived workers and tests would accumulate idle sweepers

Remediation:
- added `Close() error`
- cleanup loop now listens on a stop channel and terminates deterministically
- close is idempotent

### `internal/resolver/service.go`

Status: fixed

Finding:
- default replay-store ownership was not tracked, so the default sweeper leaked for every new service
- forwarder payload generation had to clone command/argument maps through `Command()` / `ArgumentsMap()`

Relevant snippets before fix:

```go
replay: capability.NewMemoryReplayStore(),
```

```go
return invocation.Command(), nil
```

Impact:
- library users creating transient services leaked background work
- REST and GraphQL forwarding paid unnecessary alloc/copy overhead on every request

Remediation:
- service now tracks and closes its owned default replay store
- added `Service.Close()`
- added `Invocation.MarshalArgumentsJSON()` and `Invocation.MarshalCommandJSON()` so callers can serialize the cached decoded arguments directly without cloning

### `resolver_forward.go`

Status: fixed

Finding:
- forwarding path rebuilt full command/argument maps on each request

Relevant snippet before fix:

```go
arguments, err := invocation.ArgumentsMap()
payload, err := json.Marshal(arguments)
```

Impact:
- avoidable per-request heap churn in the highest-throughput API bridge path

Remediation:
- REST payloads now use `Invocation.MarshalArgumentsJSON()` / `Invocation.MarshalCommandJSON()`
- GraphQL request generation now carries `json.RawMessage` variables instead of rebuilding intermediate maps

### `internal/clermwire/value.go`

Status: fixed

Finding:
- JSON build path copied payload bytes through `string(payloadJSON)` and then back to `[]byte`

Relevant snippet before fix:

```go
trimmed := strings.TrimSpace(string(payloadJSON))
if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
```

Impact:
- extra allocations on every request/response build path

Remediation:
- switched to `bytes.TrimSpace(payloadJSON)` and unmarshalling the original byte slice directly

### `internal/schema/schema.go`

Status: fixed

Findings:
- fingerprint cache retained documents forever through an unbounded global cache
- parameter duplicate validation was `O(n^2)`

Relevant snippets before fix:

```go
type FingerprintCache struct {
    values sync.Map
}
```

```go
for j := 0; j < i; j++ {
    if params[j].Name == name {
```

Impact:
- embedded processes compiling/loading many schemas could retain stale indexes indefinitely
- validation cost grew quadratically with parameter count

Remediation:
- replaced the unbounded cache with a bounded cache
- replaced duplicate detection with a set-based pass

### `compiler_vendor.go`

Status: fixed

Finding:
- compiler document index cache retained schema indexes forever

Relevant snippet before fix:

```go
type compilerDocumentIndexCache struct {
    values sync.Map
}
```

Impact:
- long-lived library users could accumulate unreclaimable tool/index state as schemas were loaded dynamically

Remediation:
- replaced global unbounded cache with a bounded cache guarded by `sync.RWMutex`

### `internal/capability/token.go`

Status: fixed

Finding:
- `VerifyTime()` re-ran full structural validation after `Decode()` and `Keyring.Verify()`

Relevant snippet before fix:

```go
func AssertTimeWindow(token *Token, now time.Time, skew time.Duration) error {
    if err := Validate(token); err != nil {
```

Impact:
- repeated list scans and validation work in the capability-verified request path

Remediation:
- temporal verification now checks only the time-window invariants it owns
- cryptographic and structural verification stay in `Decode()` / `Keyring.Verify()`

## Reviewed with no new actionable change in this pass

- `registryrpc/client.go`
  - response-size caps and bounded body reads were already present
- `internal/app/resolvercli/cli.go`
  - Unix-socket replacement guard and HTTP server timeouts were already present
- `internal/netutil/http_client.go`
  - transport timeouts and connection pooling were already sensible for current usage
- `internal/resolver/daemon.go`
  - no new direct RCE, fork-bomb, or payload-exfil vector confirmed in current implementation

## Verification added for this review

- request file permission regression test
- replay-store shutdown idempotency test
- resolver service shutdown idempotency test
- resolver JSON serialization benchmarks
- CLERM wire build benchmark
