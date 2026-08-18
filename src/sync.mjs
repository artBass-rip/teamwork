import {readFileSync, writeFileSync, mkdirSync} from 'node:fs';
import {dirname} from 'node:path';
import {McpClient} from './mcp-client.mjs';
import {resolveInside} from './security.mjs';

const textOf = result => result.content?.find(item => item.type === 'text')?.text;

export const DEFAULT_GROUPING = {
  labelGroupPrefix: 'Метка',
  sourceWeights: {title: 8, labels: 7, components: 7, description: 1},
  themes: [], fallbackTheme: 'Прочее'
};
const SPRINT_PLACEMENT = {order: ['active', 'future', 'backlog'], labels: {active: 'Активный спринт', future: 'Будущий спринт', backlog: 'Бэклог'}};

const DOCUMENT_DEFAULTS = {
  includeSummary: true, includeDescription: true, includeAssignee: true, includeStatus: true, includeIssueType: true, includeParent: true,
  issueTypeIcons: {Epic: '⚡', Task: '☑', Story: '📖', Bug: '🐞', 'Sub-task': '◻', Goal: '◆', 'Эпик': '⚡', 'Задача': '☑', 'История': '📖', 'Ошибка': '🐞', 'Подзадача': '◻'}, fallbackIssueTypeIcon: '•'
};

function normalizeDescription(value) {
  const raw = typeof value === 'string' ? value : JSON.stringify(value ?? '', null, 2);
  if (!raw.trim()) return '_Описание отсутствует._';
  return raw.replace(/^#{1,6}\s+(.+)$/gm, (_, title) => `###### ${title}`);
}

function matchCount(pattern, value) {
  if (!value) return 0;
  const expression = new RegExp(pattern, 'gi');
  let count = 0;
  while (expression.exec(String(value)) && count < 2) {
    count += 1;
    if (expression.lastIndex === 0) expression.lastIndex += 1;
  }
  return count;
}

export function themeFor(issue, config) {
  const weights = config.grouping.sourceWeights || {title: 8, labels: 7, components: 7, description: 1};
  const sources = {
    title: issue.title || '',
    labels: (issue.labels || []).join(' '),
    components: (issue.components || []).join(' '),
    description: issue.description || ''
  };
  let winner;
  let bestScore = 0;
  for (const theme of config.grouping.themes) {
    if (theme.issueKeys?.includes(issue.key)) return theme.name;
    const score = Object.entries(sources).reduce(
      (total, [source, value]) => total + matchCount(theme.pattern, value) * (weights[source] ?? 0),
      0
    );
    if (score > bestScore) {
      winner = theme.name;
      bestScore = score;
    }
  }
  return winner || config.grouping.fallbackTheme;
}

export function groupFor(issue, config, labelsByIssue = {}) {
  const label = labelsByIssue[issue.key]?.[0];
  return label ? `${config.grouping.labelGroupPrefix || 'Метка'}: ${label}` : themeFor(issue, config);
}

export function groupsFor(issue, config, labelsByIssue = {}) {
  const labels = labelsByIssue[issue.key] || [];
  if (labels.length) return labels.map(label => `${config.grouping.labelGroupPrefix || 'Метка'}: ${label}`);
  return [themeFor(issue, config)];
}

function placementFor(issue, config) {
  const active = issue.sprints.find(s => s.state === 'active');
  if (active) return {key: 'active', label: `${SPRINT_PLACEMENT.labels.active} — ${active.name}`};
  const future = issue.sprints.find(s => s.state === 'future');
  if (future) return {key: 'future', label: `${SPRINT_PLACEMENT.labels.future} — ${future.name}`};
  return {key: 'backlog', label: SPRINT_PLACEMENT.labels.backlog};
}

function taskMarkdown(issue, config, labelsByIssue, level = 3) {
  const jiraBaseUrl = String(config.jira.baseUrl || '').replace(/\/$/, '');
  const icon = config.document.issueTypeIcons?.[issue.type] || config.document.fallbackIssueTypeIcon || '•';
  const title = jiraBaseUrl ? `[${issue.key}](${jiraBaseUrl}/browse/${issue.key})` : issue.key;
  let md = `${'#'.repeat(level)} ${icon} ${title} — ${issue.title} — ${issue.status}\n\n`;
  if (config.document.includeAssignee) md += `- Исполнитель: ${issue.assignee}\n`;
  if (config.document.includeStatus) md += `- Статус: ${issue.status}\n`;
  if (config.document.includeIssueType) md += `- Тип: ${issue.type}\n`;
  if (labelsByIssue[issue.key]?.length) md += `- Метки TeamWork: ${labelsByIssue[issue.key].join(', ')}\n`;
  if (config.document.includeParent && issue.parent) md += `- Родительская задача: ${jiraBaseUrl ? `[${issue.parent}](${jiraBaseUrl}/browse/${issue.parent})` : issue.parent}\n`;
  if (config.document.includeDescription) md += `\n**Описание**\n\n${normalizeDescription(issue.description)}\n\n`;
  return md;
}

function preamble(config, issues, suffix) { return `# ${config.document.title} — ${suffix}\n\nДата актуализации: ${new Date().toISOString()}  \nИсточник: Jira через встроенный TeamWork MCP  \nВсего задач: **${issues.length}**\n\n`; }

export function renderBacklog(issues, config, labelsByIssue = {}) {
  let md = preamble(config, issues, 'Backlog');
  const grouped = new Map(); const flat = [];
  for (const issue of issues) {
    const explicit = labelsByIssue[issue.key]?.length || config.grouping.themes.length;
    if (!explicit) { flat.push(issue); continue; }
    for (const group of groupsFor(issue, config, labelsByIssue)) {
      if (!grouped.has(group)) grouped.set(group, []);
      grouped.get(group).push(issue);
    }
  }
  if (flat.length) { md += `## Задачи (${flat.length})\n\n`; for (const issue of flat.sort(issueOrder)) md += taskMarkdown(issue, config, labelsByIssue); }
  for (const [group, tasks] of [...grouped].sort(([a],[b]) => a.localeCompare(b, 'ru'))) {
    md += `## ${group} (${tasks.length})\n\n`;
    for (const issue of tasks.sort(issueOrder)) md += taskMarkdown(issue, config, labelsByIssue);
  }
  return md;
}

export function renderSprints(issues, config, labelsByIssue = {}) {
  let md = preamble(config, issues, 'Sprints'); const placements = new Map();
  for (const issue of issues) { const place = placementFor(issue, config); if (!placements.has(place.key)) placements.set(place.key, new Map()); const named = placements.get(place.key); if (!named.has(place.label)) named.set(place.label, []); named.get(place.label).push(issue); }
  for (const key of SPRINT_PLACEMENT.order) for (const [placement, tasks] of placements.get(key) || []) { md += `## ${placement} (${tasks.length})\n\n`; for (const issue of tasks.sort(issueOrder)) md += taskMarkdown(issue, config, labelsByIssue); }
  return md;
}
const issueOrder = (a,b) => a.key.localeCompare(b.key, undefined, {numeric:true});

export async function synchronize(configPath, mcpUrl, mcpAuthToken = '', dataDir = 'data', labelsByIssue = {}, log = () => {}, workspace = {}) {
  const config = JSON.parse(readFileSync(configPath, 'utf8'));
  validateSyncConfig(config);
  config.jira ||= {};
  config.jira.baseUrl = workspace.jiraBaseUrl || config.jira.baseUrl || '';
  config.jira.cloudId = workspace.cloudId || config.jira.cloudId || '';
  log('sync.config_loaded', 'Конфигурация синхронизации загружена', {project: config.jira.projectKey});
  const mcp = new McpClient(mcpUrl, mcpAuthToken);
  log('sync.mcp_initialize', 'Инициализация встроенного MCP-клиента', {mcpUrl});
  await mcp.initialize();
  log('sync.mcp_ready', 'MCP-клиент инициализирован');
  const projects = projectsForConfig({projects: workspace.projects});
  const allIssues = [];
  const documents = {};
  for (const project of projects) {
    config.grouping = groupingSettings(workspace.groupings?.[project.key]);
    config.document = documentForProject(project);
    const jql = jqlForProject(config.jira, project.key);
    const result = await mcp.request('tools/call', {name: 'jira_export_snapshot', arguments: {cloudId: config.jira.cloudId, jql, goalFieldName: config.jira.goalFieldName, sprintFieldId: config.jira.sprintFieldId}});
    const responseText = textOf(result);
    if (result.isError) throw new Error(`${project.key}: ${responseText || 'Embedded Jira MCP tool failed'}`);
    const raw = result.structuredContent || JSON.parse(responseText);
    const issues = raw.issues || raw;
    allIssues.push(...issues);
    const backlogOutput = resolveInside(dataDir, `data/${project.key}-backlog.md`);
    const sprintsOutput = resolveInside(dataDir, `data/${project.key}-sprints.md`);
    const issuesOutput = resolveInside(dataDir, `data/${project.key}-issues.json`);
    mkdirSync(dirname(backlogOutput), {recursive: true});
    writeFileSync(backlogOutput, renderBacklog(issues, config, labelsByIssue));
    writeFileSync(sprintsOutput, renderSprints(issues, config, labelsByIssue));
    writeFileSync(issuesOutput, `${JSON.stringify(issues)}\n`);
    documents[project.key] = {key: project.key, name: project.name, backlogOutput, sprintsOutput, issues: issues.length};
    log('sync.project_written', 'Документы Jira-проекта сохранены', {project: project.key, issues: issues.length, backlogOutput, sprintsOutput});
  }
  log('sync.tool_ready', 'Snapshots выбранных Jira-проектов получены', {projects: projects.length});
  return {issues: allIssues.length, issueKeys: allIssues.map(issue => issue.key), documents, output: Object.values(documents)[0]?.backlogOutput, updatedAt: new Date().toISOString()};
}

export function jqlForProject(jira, projectKey) {
  if (typeof projectKey !== 'string' || !/^[A-Z][A-Z0-9_]*$/.test(projectKey)) throw new Error('Jira project key is missing or invalid');
  const escapedKey = projectKey.replace(/"/g, '\\"');
  const jql = jira.jqlTemplate
    ? jira.jqlTemplate.replaceAll('{project}', escapedKey)
    : jira.jql
      ? String(jira.jql).replace(/project\s*=\s*(?:"[^"]+"|[A-Z][A-Z0-9_]*)/i, `project="${escapedKey}"`)
      : `project="${escapedKey}" AND statusCategory!=Done ORDER BY created DESC`;
  if (!jql.trim()) throw new Error(`JQL is not configured for project ${projectKey}`);
  return jql;
}

export function projectsForConfig(jira = {}) {
  const raw = jira.projects?.length ? jira.projects : jira.projectKey ? [{key: jira.projectKey, name: jira.projectKey}] : [];
  const projects = raw.map(project => ({key: String(project?.key || '').trim().toUpperCase(), name: String(project?.name || project?.key || '').trim()})).filter(project => /^[A-Z][A-Z0-9_]*$/.test(project.key));
  if (!projects.length) throw new Error('No valid Jira projects are selected. Open Projects and select at least one project.');
  return [...new Map(projects.map(project => [project.key, project])).values()];
}

export function validateSyncConfig(config) {
  if (!config?.schedule || !config?.logging) throw new Error('Configuration is incomplete: schedule and logging sections are required');
  return config;
}

export function groupingSettings(value) {
  const grouping = {...DEFAULT_GROUPING, ...(value || {}), sourceWeights: {...DEFAULT_GROUPING.sourceWeights, ...(value?.sourceWeights || {})}};
  if (!Array.isArray(grouping.themes)) throw new Error('Grouping settings must contain a themes array');
  for (const theme of grouping.themes) new RegExp(theme.pattern, 'i');
  return grouping;
}

export function documentForProject(project) {
  return {...DOCUMENT_DEFAULTS, title: `${project.name || project.key} — бэклог и созданные спринты`, outputPath: `data/${project.key}-jira-backlog-and-sprints.md`};
}
