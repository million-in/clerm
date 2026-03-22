# Other Language Servers

These examples show how to host CLERM-compatible endpoints before official SDKs exist.

- `go/`: embeds the resolver in-process
- `typescript/`: forwards CLERM requests to the local daemon for decode and response encode
- `python/`: same daemon pattern using the Python standard library
- `rust/`: same daemon pattern using Axum and Reqwest

All examples assume a schema compatible with `examples/provider_search.clermfile`.
