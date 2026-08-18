# Changelog

All notable changes are documented here. The format follows Keep a Changelog and Semantic Versioning.

## [1.8.0] - 2026-08-17

### Added

- Discovery of every Jira project available to the connected Atlassian user through the bundled MCP server.
- A Projects workspace for selecting the projects polled and analyzed by scheduled synchronization.
- An independent document tab for each selected project with Backlog and Sprints views.
- A Russian/English interface switch in the application header with the preference stored in the browser.

### Changed

- Synchronization now creates one Markdown snapshot per selected project while retaining the legacy output for single-project installations.
- The OAuth encryption key now lives beside the encrypted session in the private Docker volume, preventing key drift across project moves and container recreation; unreadable legacy sessions are quarantined instead of crashing the MCP healthcheck.
- Selected projects and the optional Jira base URL moved out of grouping configuration into `data/workspace.json`; UI values are authoritative and the Integration tab can update the Jira address.
- `document` and `grouping` are no longer required configuration sections: document metadata is derived per project, while grouping defaults are edited and persisted through the Configuration workspace.
- Backlog is now sprint-free and flat without explicit rules; project groupings are isolated, Sprints uses its own generated document, and an append-only analyzer can add candidate themes from synchronized issue metadata.
- Analyzer-owned themes are now reevaluated on every run: manual themes remain intact, while rules marked `source: "analyzer"` are rebuilt from the current project snapshot.
- The keyword analyzer was replaced with semantic LLM classification. Ollama is the default provider; an encrypted OpenAI-compatible external connection and an explicit provider switch are available under Integration.

## [1.7.0] - 2026-08-16

### Added

- A bundled, read-only Jira MCP sidecar built on the official MCP SDK.
- Browser-only Atlassian OAuth 2.1 through the official Rovo MCP, using PKCE and dynamic client registration without Client ID or Client Secret input.
- AES-256-GCM token storage in a private Docker volume with a launcher-generated encryption key.
- The bundled Jira MCP is now the sole provider; legacy Docker MCP Gateway startup and code-mode execution were removed.
- Container authentication, origin validation, rate-limit retries, health checks, tests, and dependency auditing for the new service.

## [1.6.0] - 2026-08-16

### Added

- A copy action on every task heading that writes both rich HTML and plain text to the clipboard.
- Preserved clickable Jira hyperlinks for Confluence and OneNote, with explicit URLs in plain-text destinations.
- Visual success feedback, legacy clipboard fallback, and automated coverage for URL preservation.

## [1.5.0] - 2026-08-15

### Added

- Existing-label suggestions and a reusable label catalog in the task panel.
- Label usage counts and confirmed system-wide label deletion with automatic document regrouping.
- Store-level tests for catalog generation and case-insensitive global deletion.

## [1.4.0] - 2026-08-15

### Added

- A separate Sprints tab using the existing editor-style viewer, outline, search, folding, comments, and task labels.
- Client-side hierarchy transformation from Goal → workstream → placement to placement → Goal → workstream without another Jira request.
- Automated coverage for sprint ordering and hierarchy preservation.

## [1.3.0] - 2026-07-29

### Changed

- Replaced first-regex theme selection with weighted classification across title, Jira labels, components, and description.
- Introduced workstreams for runtime platform, developer platform, cloud/IaC, security, observability, data, networking, reliability, FinOps, research, and legacy work.
- A task with local labels is now displayed in every matching label group; labels still override themes completely.
- Kept Goal as the mandatory top-level group, including the configured empty-Goal group, and retained active/future/backlog placement order.
- Goal counters now count unique issues when a task belongs to multiple label groups.
- The macOS launcher now waits for the previous launchd Gateway job to exit before reusing its label and log.

## [1.2.0] - 2026-07-29

### Added

- Local task labels with task-panel management and inline label chips.
- Priority ordering: the first label overrides theme grouping during document generation.
- Automatic document regrouping after label changes and orphan-label cleanup after synchronization.
- Label storage and grouping behavior tests.

## [1.1.0] - 2026-07-29

### Changed

- Renamed the project and repository to TeamWork.
- Bound the web service to localhost by default and added optional HTTP Basic authentication.
- Restricted generated-document access to the runtime data directory.
- Hardened the container with a read-only root filesystem, dropped capabilities, `no-new-privileges`, and a healthcheck.
- Added behavioral tests for authentication, path confinement, and local comments.
- Validated stored Gateway PIDs before terminating a previous process.
- Moved the macOS Gateway lifecycle to `launchd`, independent of the launching terminal.
- Rejected direct Compose startup without an MCP token and supplied Docker Desktop credential-helper paths to launchd.

## [1.0.0] - 2026-07-29

### Added

- Docker MCP Gateway integration with profile-managed Atlassian OAuth.
- Scheduled Jira synchronization and configurable Markdown generation.
- Goal-first grouping with theme and sprint/backlog placement levels.
- Editor-style web viewer with outline, search, scroll tracking, and folding.
- Local task comments with inline display, side-panel management, and orphan cleanup.
- JSONL logging and in-app log viewer.
- English and Russian documentation.
- CodeQL, dependency review, npm audit, Dependabot, and repository security settings.
