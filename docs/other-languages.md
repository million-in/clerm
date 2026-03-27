# Other Language Samples

The sample servers live in `other_servers/`.

- `other_servers/go`: embedded in-process resolver
- `other_servers/typescript`: Node HTTP server using the local daemon
- `other_servers/python`: stdlib HTTP server using the local daemon
- `other_servers/rust`: Rust HTTP server using the local daemon

The non-Go examples all follow the same pattern:

1. receive CLERM bytes on the normal application route
2. send those bytes to `clerm-resolver /v1/requests/decode`
3. dispatch on `command.method`
4. send output JSON to `clerm-resolver /v1/responses/encode`
5. write the returned CLERM bytes back to the caller

This keeps CLERM binary parsing, token verification, and response encoding in one place until each language gets a native SDK.

The daemon HTTP contract used by these examples is covered by the compatibility
tests under `tests/compatibility/clerm`.
