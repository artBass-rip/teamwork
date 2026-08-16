import {readFileSync, writeFileSync, mkdirSync} from 'node:fs';
import {dirname} from 'node:path';
import {McpClient} from './mcp-client.mjs';
import {resolveInside} from './security.mjs';

const textOf = result => result.content?.find(item => item.type === 'text')?.text;

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
  if (active) return {key: 'active', label: `${config.grouping.placementLabels.active} — ${active.name}`};
  const future = issue.sprints.find(s => s.state === 'future');
  if (future) return {key: 'future', label: `${config.grouping.placementLabels.future} — ${future.name}`};
  return {key: 'backlog', label: config.grouping.placementLabels.backlog};
}

function buildTree(issues, config, labelsByIssue) {
  const tree = new Map();
  for (const issue of issues) {
    const goal = issue.goal || config.grouping.emptyGoalLabel;
    const placement = placementFor(issue, config);
    if (!tree.has(goal)) tree.set(goal, new Map());
    for (const theme of groupsFor(issue, config, labelsByIssue)) {
      if (!tree.get(goal).has(theme)) tree.get(goal).set(theme, new Map());
      if (!tree.get(goal).get(theme).has(placement.key)) tree.get(goal).get(theme).set(placement.key, {label: placement.label, issues: []});
      tree.get(goal).get(theme).get(placement.key).issues.push(issue);
    }
  }
  return tree;
}

function render(issues, config, labelsByIssue) {
  const tree = buildTree(issues, config, labelsByIssue);
  const jiraBaseUrl = String(config.jira.baseUrl || '').replace(/\/$/, '');
  let md = `# ${config.document.title}\n\n`;
  md += `Дата актуализации: ${new Date().toISOString()}  \nИсточник: Jira через Docker MCP Gateway  \nВсего задач: **${issues.length}**\n\n`;
  for (const [goal, themes] of [...tree].sort(([a], [b]) => a.localeCompare(b, 'ru'))) {
    const goalCount = new Set(
      [...themes.values()].flatMap(placements => [...placements.values()].flatMap(group => group.issues.map(issue => issue.key)))
    ).size;
    md += `## Goal: ${goal} (${goalCount})\n\n`;
    for (const [theme, placements] of [...themes].sort(([a], [b]) => a.localeCompare(b, 'ru'))) {
      const themeCount = [...placements.values()].reduce((n, x) => n + x.issues.length, 0);
      md += `### ${theme} (${themeCount})\n\n`;
      for (const key of config.grouping.placementOrder) {
        const group = placements.get(key);
        if (!group) continue;
        md += `#### ${group.label} (${group.issues.length})\n\n`;
        for (const issue of group.issues.sort((a, b) => a.key.localeCompare(b.key, undefined, {numeric: true}))) {
          const typeIcon = config.document.issueTypeIcons?.[issue.type] || config.document.fallbackIssueTypeIcon || '•';
          md += `##### ${typeIcon} [${issue.key}](${jiraBaseUrl}/browse/${issue.key}) — ${issue.title} — ${issue.status}\n\n`;
          if (config.document.includeAssignee) md += `- Исполнитель: ${issue.assignee}\n`;
          if (config.document.includeStatus) md += `- Статус: ${issue.status}\n`;
          if (config.document.includeIssueType) md += `- Тип: ${issue.type}\n`;
          if (labelsByIssue[issue.key]?.length) md += `- Метки TeamWork: ${labelsByIssue[issue.key].join(', ')}\n`;
          if (config.document.includeParent && issue.parent) md += `- Родительская задача: [${issue.parent}](${jiraBaseUrl}/browse/${issue.parent})\n`;
          if (config.document.includeDescription) md += `\n**Описание**\n\n${normalizeDescription(issue.description)}\n\n`;
        }
      }
    }
  }
  return md;
}

export async function synchronize(configPath, mcpUrl, mcpAuthToken = '', dataDir = 'data', labelsByIssue = {}, log = () => {}) {
  const config = JSON.parse(readFileSync(configPath, 'utf8'));
  log('sync.config_loaded', 'Конфигурация синхронизации загружена', {project: config.jira.projectKey});
  const mcp = new McpClient(mcpUrl, mcpAuthToken);
  log('sync.mcp_initialize', 'Инициализация MCP-клиента', {mcpUrl});
  await mcp.initialize();
  log('sync.mcp_ready', 'MCP-клиент инициализирован');
  await mcp.request('tools/call', {name: 'code-mode', arguments: {name: 'jira-do-sync', servers: ['atlassian-remote']}});
  log('sync.tool_ready', 'Code-mode инструмент Atlassian подготовлен');
  const remoteScript = `
const sample=JSON.parse(getJiraIssue({cloudId:${JSON.stringify(config.jira.cloudId)},issueIdOrKey:'DO-2241',fields:['*all'],expand:'names',responseContentFormat:'markdown'}));
const goalFields=Object.entries(sample.names||{}).filter(([,name])=>String(name).trim().toLowerCase()===${JSON.stringify(config.jira.goalFieldName.toLowerCase())}).map(([id])=>id);
let token,all=[];do{const r=JSON.parse(searchJiraIssuesUsingJql({cloudId:${JSON.stringify(config.jira.cloudId)},jql:${JSON.stringify(config.jira.jql)},fields:['summary','description','assignee','status','issuetype','parent','labels','components',${JSON.stringify(config.jira.sprintFieldId)}].concat(goalFields),maxResults:100,nextPageToken:token,responseContentFormat:'markdown',searchResultMode:'issues'}));all=all.concat(r.issues);token=r.nextPageToken}while(token);
return JSON.stringify(all.map(i=>({key:i.key,title:i.fields.summary||'',description:i.fields.description||'',assignee:i.fields.assignee?.displayName||'Не назначен',status:i.fields.status?.name||'',type:i.fields.issuetype?.name||'',parent:i.fields.parent?.key||'',labels:i.fields.labels||[],components:(i.fields.components||[]).map(c=>c.name),goal:(()=>{const v=goalFields.map(id=>i.fields[id]).find(x=>x!=null&&(!Array.isArray(x)||x.length));if(v==null)return '';if(Array.isArray(v))return v.map(x=>x?.value||x?.name||x?.displayName||String(x)).join(', ');if(typeof v==='object')return v.value||v.name||v.displayName||JSON.stringify(v);return String(v)})(),sprints:(i.fields[${JSON.stringify(config.jira.sprintFieldId)}]||[]).map(s=>({id:s.id,name:s.name,state:s.state}))})));`;
  const result = await mcp.request('tools/call', {name: 'mcp-exec', arguments: {name: 'code-mode-jira-do-sync', arguments: {script: remoteScript}}});
  const raw = JSON.parse(textOf(result));
  const issues = Array.isArray(raw) ? raw : JSON.parse(textOf(raw));
  log('sync.issues_received', 'Задачи Jira получены', {issues: issues.length});
  const output = resolveInside(dataDir, config.document.outputPath);
  mkdirSync(dirname(output), {recursive: true});
  writeFileSync(output, render(issues, config, labelsByIssue));
  log('sync.document_written', 'Markdown-документ сохранён', {issues: issues.length, output});
  return {issues: issues.length, issueKeys: issues.map(issue => issue.key), output, updatedAt: new Date().toISOString()};
}
