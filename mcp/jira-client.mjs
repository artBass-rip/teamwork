import {Client, StreamableHTTPClientTransport} from '@modelcontextprotocol/client';

const textOf = result => result.content?.find(item => item.type === 'text')?.text || '';
const parseResult = result => {
  if (result.isError) throw new Error(textOf(result) || 'Atlassian Rovo MCP tool failed');
  if (result.structuredContent) return result.structuredContent;
  const text = textOf(result);
  try { return JSON.parse(text); } catch { return text; }
};

function sprintOf(value) {
  if (value && typeof value === 'object') return {id: value.id, name: value.name || '', state: String(value.state || '').toLowerCase()};
  const text = String(value || '');
  return {id: Number(/id=(\d+)/.exec(text)?.[1]) || null, name: /name=([^,\]]+)/.exec(text)?.[1] || text, state: (/state=([^,\]]+)/.exec(text)?.[1] || '').toLowerCase()};
}

function fieldValue(value) {
  if (value == null) return '';
  if (Array.isArray(value)) return value.map(fieldValue).filter(Boolean).join(', ');
  if (typeof value === 'object') return value.value || value.name || value.displayName || value.key || '';
  return String(value);
}

export class JiraClient {
  constructor(oauthProvider, {serverUrl = 'https://mcp.atlassian.com/v1/mcp/authv2'} = {}) { this.oauthProvider = oauthProvider; this.serverUrl = serverUrl; }

  async withClient(action) {
    if (!this.oauthProvider.status().connected) throw new Error('Atlassian is not connected');
    const client = new Client({name: 'teamwork-jira-adapter', version: '0.2.0'});
    const transport = new StreamableHTTPClientTransport(new URL(this.serverUrl), {authProvider: this.oauthProvider});
    try { await client.connect(transport); return await action(client); } finally { await client.close().catch(() => {}); }
  }

  async call(name, args) { return this.withClient(async client => parseResult(await client.callTool({name, arguments: args}))); }

  async search({jql, fields, maxResults = 100, cloudId = ''}) {
    let nextPageToken;
    const issues = [];
    do {
      const page = await this.call('searchJiraIssuesUsingJql', {cloudId, jql, fields, maxResults, nextPageToken, responseContentFormat: 'markdown', searchResultMode: 'issues'});
      const value = typeof page === 'string' ? JSON.parse(page) : page;
      issues.push(...(value.issues || []));
      nextPageToken = value.nextPageToken;
    } while (nextPageToken);
    return issues;
  }

  async exportSnapshot({jql, goalFieldName = 'Goal', sprintFieldId = 'customfield_10020', cloudId = ''}) {
    const baseFields = ['summary', 'description', 'assignee', 'status', 'issuetype', 'parent', 'labels', 'components', sprintFieldId];
    const initialIssues = await this.search({jql, fields: baseFields, cloudId});
    if (!initialIssues.length) return [];
    const probe = await this.call('getJiraIssue', {cloudId, issueIdOrKey: initialIssues[0].key, fields: ['*all'], expand: 'names', responseContentFormat: 'markdown'}).catch(() => ({names: {}}));
    const sample = typeof probe === 'string' ? JSON.parse(probe) : probe;
    const goalFieldIds = Object.entries(sample.names || {}).filter(([, name]) => String(name).trim().toLowerCase() === goalFieldName.trim().toLowerCase()).map(([id]) => id);
    const issues = goalFieldIds.length ? await this.search({jql, fields: [...baseFields, ...goalFieldIds], cloudId}) : initialIssues;
    return issues.map(issue => {
      const value = issue.fields || {};
      const goal = goalFieldIds.map(id => value[id]).find(item => item != null && (!Array.isArray(item) || item.length));
      return {key: issue.key, title: value.summary || '', description: value.description || '', assignee: value.assignee?.displayName || 'Не назначен', status: value.status?.name || '', type: value.issuetype?.name || '', parent: value.parent?.key || '', labels: value.labels || [], components: (value.components || []).map(component => component.name), goal: fieldValue(goal), sprints: (value[sprintFieldId] || []).map(sprintOf)};
    });
  }

  async issue(key, cloudId = '') { return this.call('getJiraIssue', {cloudId, issueIdOrKey: key, fields: ['*all'], expand: 'names', responseContentFormat: 'markdown'}); }
  async sites() {
    const result = await this.call('getAccessibleAtlassianResources', {});
    const sites = Array.isArray(result) ? result : result.resources || result.values || [];
    return sites.map(site => ({id: String(site.id || ''), name: site.name || site.url || '', url: site.url || ''})).filter(site => site.id);
  }
  async projects(cloudId = '') {
    const projects = [];
    let startAt = 0;
    let done = false;
    while (!done) {
      const result = await this.call('getVisibleJiraProjects', {cloudId, action: 'browse', maxResults: 50, startAt});
      const value = typeof result === 'string' ? JSON.parse(result) : result;
      const page = value.values || value.projects || (Array.isArray(value) ? value : []);
      projects.push(...page);
      startAt += page.length;
      done = Boolean(value.isLast) || !page.length || (value.total != null ? startAt >= Number(value.total) : page.length < 50);
    }
    return projects.map(project => ({id: String(project.id || ''), key: project.key, name: project.name || project.key, type: project.projectTypeKey || project.type || ''})).filter(project => project.key);
  }
}
