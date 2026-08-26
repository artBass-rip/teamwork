# TeamWork module protocol v1

Modules are portable executables. The core starts each module as a separate subprocess and supplies three environment variables:

- `TEAMWORK_CORE_SOCKET`: Unix domain socket path;
- `TEAMWORK_MODULE_ID`: manifest module identifier;
- `TEAMWORK_MODULE_TOKEN`: random token valid only for that process invocation.

Transport is JSON-RPC 2.0 over UTF-8 NDJSON. A message may not exceed 4 MiB. The first module request must be `module.register`:

```json
{"jsonrpc":"2.0","id":"register","method":"module.register","params":{"module_id":"teamwork.example","token":"...","protocol_version":"1"}}
```

The core invokes module capabilities with `capability.invoke` and delivers durable events with `event.deliver`. Modules call the core with `capability.invoke`, `event.publish`, and `module.heartbeat`. Modules must treat event delivery and workflow actions as at-least-once and implement idempotency.

Modules must not connect to another module directly. Discovery, routing, credentials and canonical state are mediated by the core.
