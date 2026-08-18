# TeamWork

> **AI disclosure:** this project was created with the assistance of **OpenAI Codex (GPT-5)**. The maintainer reviewed and accepted the resulting implementation and documentation.

TeamWork periodically retrieves Jira issues through its bundled read-only MCP server, groups them into a navigable hierarchy, generates Markdown, and serves an editor-style web viewer.

## Features

- Bundled read-only Jira MCP sidecar based on the official MCP SDK.
- Browser-based Atlassian OAuth 2.1 through the official Atlassian Rovo MCP, with encrypted token storage.
- Multi-project discovery and selection, with scheduled polling and a separate generated document for every selected Jira project.
- Per-project workspace tabs with Backlog and Sprints views, plus a persistent Russian/English interface switch.
- Configurable grouping: Goal → local label or weighted workstream → active sprint → future sprint → backlog.
- Periodic and manual synchronization.
- Markdown viewer with document outline, search, scroll tracking, and nested folding.
- Separate Sprints tab that reverses the hierarchy to placement → Goal → workstream while preserving active, future, backlog order.
- Configurable issue-type icons and task status in headings.
- One-click rich-text task copying with a preserved Jira hyperlink for Confluence, OneNote, and plain-text fallbacks.
- Local task comments displayed below task headings and managed from a side panel.
- Local task labels with inline chips and label-first multi-grouping.
- Reusable local-label catalog with existing-label selection, per-task removal, usage counts, and confirmed global deletion.
- Independent comment storage with automatic cleanup when an issue disappears.
- Structured JSONL application logs and an in-app log viewer.
- Containerized runtime with no third-party Node.js runtime dependencies.

## Requirements

- Docker with Compose; Docker Desktop 4.62 or newer is recommended.
- An Atlassian account with access to the Atlassian Rovo MCP service.
- Node.js 22+ only for local validation outside Docker.

## Configuration

Create the private runtime configuration:

```bash
cp grouping.config.example.json grouping.config.json
```

Only scheduling and logging are required in `grouping.config.json`. Project keys and the Jira base URL do not belong in this configuration. Select polled projects from **Projects** and set the optional Jira address under **Integration**. Document names and output paths are derived per selected project. Grouping rules are independent per project and stored under `groupings.<PROJECT_KEY>` in `data/workspace.json`; no Backlog grouping is enabled by default.

### Jira MCP authorization

Start TeamWork:

```bash
./start.sh
```

Open the **Integration** tab and select **Connect Atlassian**. TeamWork opens the official Atlassian consent screen and completes OAuth 2.1 using PKCE and dynamic client registration. No Client ID, Client Secret, developer-console app, or API token is required. The encrypted OAuth session and its encryption key are retained together in a private Docker volume, so restarts, rebuilds, project moves, and normal container recreation do not require another sign-in. The local sidecar exposes only TeamWork's read operations and is not published on a host port; its official Atlassian Rovo MCP upstream is requested with read-only Jira scopes.

Available MCP tools are `jira_list_sites`, `jira_list_projects`, `jira_export_snapshot`, `jira_search_issues`, and `jira_get_issue`. The built-in server is the only supported MCP provider. The site cloud ID is discovered from the connected Atlassian account and retained as internal workspace state.

Open <http://localhost:8080> after startup.

Always use `./start.sh` to create or recreate the application containers. Direct `docker compose up` is intentionally rejected because it cannot provide the ephemeral internal MCP token and encryption key.

### Network access and authentication

By default Compose binds the application only to `127.0.0.1`. To expose it on a trusted network, enable Basic Auth and explicitly change the bind address:

```bash
APP_BIND_ADDRESS=0.0.0.0 \
APP_AUTH_USER=owner \
APP_AUTH_PASSWORD='use-a-long-random-password' \
./start.sh
```

Do not expose the service publicly without HTTPS in front of it. The password is passed at runtime and must never be committed.

## Runtime data

Generated documents, local comments, logs, and process files live under `data/`. They are intentionally excluded from version control. Local comments are never sent to Jira and never embedded into the generated Markdown.

TeamWork writes separate `<PROJECT>-backlog.md` and `<PROJECT>-sprints.md` documents. Backlog never groups by sprint: without explicit rules it is a flat issue list, and with rules it applies only that project's themes and local labels. The Sprints view remains placement-oriented. Use **Analyze** to semantically classify the synchronized project snapshot. Manual rules are preserved; rules marked `source: "analyzer"` are rebuilt from current issues on every analyzer run.

### Semantic analyzer providers

The analyzer uses the bundled Ollama service by default (`qwen2.5:3b`). Pull that model into the persistent Ollama volume before the first analysis: `./start.sh --pull-model`. Under **Integration**, an OpenAI-compatible external endpoint, model, and API key can be connected. Connection does not activate it: choose Ollama or External LLM explicitly and save. External keys are AES-256-GCM encrypted in the private `llm-secrets` Docker volume and are never written to workspace or grouping files. The LLM classifies technical meaning and returns descriptions, examples, and exact issue memberships rather than keyword regexes.

### Task labels and grouping priority

Open a task with its `💬` action or by clicking an existing label chip. Add up to ten local labels in the task panel by selecting an existing catalog entry or entering a new name. The `×` action removes a label only from the current task. The catalog shows usage counts and can delete a label from every task after confirmation. During synchronization, local labels completely override theme classification: the task is shown under every corresponding `Label: <label>` group. Tasks without local labels are classified using weighted matches in the title, Jira labels, components, and description. Configure their weights with `grouping.sourceWeights`; theme order is used only to resolve equal scores. Labels are stored in `data/labels.json`, are not sent to Jira, and are removed when their task disappears from the synchronized document.

`Goal` is always the top-level group. Issues without a Jira Goal are retained under `Goal: <emptyGoalLabel>`. Placement is always ordered as active sprint, future sprint, then backlog.

## Development

```bash
npm ci
npm run check
docker compose build
```

## Security

GitHub Actions run syntax and behavioral tests, `npm audit`, CodeQL analysis, dependency review, and Dependabot updates. The container runs as an unprivileged user with a read-only root filesystem, dropped capabilities, `no-new-privileges`, and a healthcheck. Document paths are restricted to `data/`. Secret scanning, push protection, private vulnerability reporting, and automated security updates are enabled for the public repository. See [SECURITY.md](SECURITY.md).

## Documentation

- [Русская документация](README.ru.md)
- [English changelog](CHANGELOG.en.md)
- [Журнал изменений на русском](CHANGELOG.ru.md)

## License

MIT © 2026 artBass-rip
