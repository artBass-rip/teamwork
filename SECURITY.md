# Security Policy

## Supported versions

Security fixes are provided for the latest release on the `master` branch.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub **Private vulnerability reporting** on the repository Security tab. Include reproduction steps, affected versions, impact, and any suggested mitigation.

The maintainer aims to acknowledge reports within seven days. Disclosure timing is coordinated after validation and remediation.

## Secrets and local data

Never commit `grouping.config.json`, `.env` files, generated Jira documents, local comments, logs, runtime tokens, or credentials. Browser OAuth uses the official Atlassian Rovo MCP with PKCE and dynamic client registration, so TeamWork does not collect a Client ID, Client Secret, or API token. OAuth session data is stored in an AES-256-GCM envelope in a private Docker volume; the encryption key is generated locally with mode `0600`. The ephemeral internal MCP bearer token is passed only through runtime environment variables.

The Jira MCP sidecar is not published to the host network, validates request authorization and browser origins, exposes read-only Jira tools, and runs without root privileges or Linux capabilities on a read-only root filesystem.

The web service binds to localhost by default. Network deployments must set `APP_AUTH_PASSWORD`, use a strong unique value, and terminate HTTPS in a trusted reverse proxy. Generated-document paths are confined to the runtime `data/` directory.
