# Go Sample

```bash
go run ./other_servers/go
```

Environment:

- `CLERM_SCHEMA`: local `.clermcfg` path
- `CLERM_PUBLIC_KEY`: registry public key path for verified relations
- `LISTEN_ADDR`: HTTP listen address

The server mounts the resolver middleware on `/api`.
