# Security Policy

Security fixes are provided for the latest revision on `master`. Report suspected vulnerabilities through GitHub Private Vulnerability Reporting rather than a public issue.

## Runtime boundaries

- Core binds the web interface to `127.0.0.1` by default.
- Module IPC uses a user-private Unix Domain Socket with mode `0600`.
- Every subprocess receives a new random 256-bit registration token.
- Modules communicate through the core; direct module-to-module connections are unsupported.
- RPC messages are bounded to 4 MiB.
- Secrets are not stored in SQLite. Native adapters target macOS Keychain and Linux Secret Service.
- Release binaries are built with `CGO_ENABLED=0`; users do not need language runtimes or local environments.

The current module permission manifest is descriptive. Network and secret namespace enforcement must be completed before third-party modules are treated as untrusted.
