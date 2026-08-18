import http from 'node:http';
import {readFileSync, writeFileSync, existsSync} from 'node:fs';
import {extname, resolve, sep} from 'node:path';
import {DEFAULT_GROUPING, groupingSettings, synchronize, validateSyncConfig} from './sync.mjs';
import {McpClient} from './mcp-client.mjs';
import {Logger} from './logger.mjs';
import {CommentStore} from './comments.mjs';
import {LabelStore} from './labels.mjs';
import {isAuthorized, resolveInside, securityHeaders} from './security.mjs';
import {WorkspaceStore} from './workspace.mjs';
import {LlmAnalyzer} from './llm.mjs';
import {LlmSecretStore} from './llm-secret.mjs';

const port = Number(process.env.PORT || 8080);
const configPath = resolve(process.env.CONFIG_PATH || 'grouping.config.json');
const mcpUrl = process.env.MCP_URL || 'http://jira-mcp:8081/mcp';
const mcpAuthToken = process.env.MCP_AUTH_TOKEN || '';
const jiraMcpBaseUrl = process.env.JIRA_MCP_BASE_URL || 'http://jira-mcp:8081';
const publicDir = resolve('public');
const dataDir = resolve(process.env.DATA_DIR || 'data');
const authUser = process.env.APP_AUTH_USER || 'admin';
const authPassword = process.env.APP_AUTH_PASSWORD || '';
const logger = new Logger(resolve(process.env.LOG_PATH || 'data/app.log'));
const comments = new CommentStore(resolve(process.env.COMMENTS_PATH || 'data/comments.json'), logger);
const labels = new LabelStore(resolve(process.env.LABELS_PATH || 'data/labels.json'), logger);
const workspace = new WorkspaceStore(resolve(process.env.WORKSPACE_PATH || 'data/workspace.json'));
const llmSecrets = new LlmSecretStore(resolve(process.env.LLM_SECRET_DIR || '/var/lib/teamwork-llm'));
let state = {running: false, lastSuccess: null, lastError: null, issues: null, provider: 'embedded-jira'};
let timer;

const json = (res, status, value) => {
  res.writeHead(status, {...securityHeaders, 'content-type': 'application/json; charset=utf-8'});
  res.end(JSON.stringify(value));
};

async function syncNow() {
  if (state.running) {
    logger.warn('sync.skipped', 'Синхронизация уже выполняется');
    return state;
  }
  const runId = crypto.randomUUID();
  state = {...state, running: true, lastError: null};
  logger.info('sync.started', 'Синхронизация запущена', {runId, mcpUrl});
  try {
    const scope = await ensureWorkspaceSite();
    const result = await synchronize(configPath, mcpUrl, mcpAuthToken, dataDir, labels.all(), (event, message, context = {}) => logger.info(event, message, {runId, ...context}), scope);
    const pruned = comments.prune(result.issueKeys);
    if (pruned.length) logger.info('comments.pruned', 'Удалены комментарии отсутствующих в документе задач', {runId, issues: pruned.length, comments: pruned.reduce((sum, item) => sum + item.comments, 0)});
    const prunedLabels = labels.prune(result.issueKeys);
    if (prunedLabels.length) logger.info('labels.pruned', 'Удалены метки отсутствующих в документе задач', {runId, issues: prunedLabels.length});
    state = {...state, running: false, lastSuccess: result.updatedAt, issues: result.issues, output: result.output, documents: result.documents};
    logger.info('sync.completed', 'Синхронизация успешно завершена', {runId, issues: result.issues, output: result.output});
  } catch (error) {
    state = {...state, running: false, lastError: error.message};
    logger.error('sync.failed', 'Синхронизация завершилась ошибкой', {runId, error: error.message, cause: error.cause?.message});
  }
  return state;
}

function schedule() {
  clearInterval(timer);
  const config = JSON.parse(readFileSync(configPath, 'utf8'));
  logger.configure(config.logging);
  timer = setInterval(syncNow, Math.max(1, config.schedule.intervalMinutes) * 60_000);
  logger.info('schedule.configured', 'Расписание синхронизации настроено', {intervalMinutes: config.schedule.intervalMinutes, runOnStart: config.schedule.runOnStart});
  if (config.schedule.runOnStart) syncNow();
}

function body(req) {
  return new Promise((resolve, reject) => {
    let value = '';
    req.on('data', chunk => { value += chunk; if (value.length > 2_000_000) req.destroy(); });
    req.on('end', () => resolve(value)); req.on('error', reject);
  });
}

async function jiraMcpRequest(path, options = {}) {
  const response = await fetch(`${jiraMcpBaseUrl}${path}`, {redirect: 'manual', ...options, headers: {authorization: `Bearer ${mcpAuthToken}`, ...(options.body ? {'content-type': 'application/json'} : {}), ...options.headers}});
  const text = await response.text();
  const value = text ? JSON.parse(text) : {};
  if (!response.ok) throw new Error(value.error || `Jira MCP HTTP ${response.status}`);
  return value;
}

async function visibleProjects() {
  const current = await ensureWorkspaceSite();
  const client = new McpClient(mcpUrl, mcpAuthToken);
  await client.initialize();
  const result = await client.request('tools/call', {name: 'jira_list_projects', arguments: {cloudId: current.cloudId}});
  const text = result.content?.find(item => item.type === 'text')?.text || '{}';
  if (result.isError) throw new Error(text);
  return (result.structuredContent || JSON.parse(text)).projects || [];
}

async function ensureWorkspaceSite() {
  const config = JSON.parse(readFileSync(configPath, 'utf8'));
  let current = workspace.get();
  let cloudId = current.cloudId || config.jira?.cloudId || '';
  if (!cloudId) {
    const client = new McpClient(mcpUrl, mcpAuthToken);
    await client.initialize();
    const sitesResult = await client.request('tools/call', {name: 'jira_list_sites', arguments: {}});
    const sitesText = sitesResult.content?.find(item => item.type === 'text')?.text || '{}';
    if (sitesResult.isError) throw new Error(sitesText);
    const sites = (sitesResult.structuredContent || JSON.parse(sitesText)).sites || [];
    const wantedHost = current.jiraBaseUrl ? new URL(current.jiraBaseUrl).host : '';
    const site = sites.find(item => item.url && new URL(item.url).host === wantedHost) || sites[0];
    if (!site?.id) throw new Error('No accessible Atlassian site was found for the connected account');
    current = workspace.update({cloudId: site.id, jiraBaseUrl: current.jiraBaseUrl || site.url});
  }
  return current.cloudId ? current : workspace.update({cloudId});
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`);
  if (url.pathname === '/api/health') return json(res, 200, {status: 'ok'});
  if (!isAuthorized(req.headers.authorization, authUser, authPassword)) {
    res.writeHead(401, {...securityHeaders, 'www-authenticate': 'Basic realm="TeamWork", charset="UTF-8"'});
    return res.end('Authentication required');
  }
  const commentPath = /^\/api\/comments\/([A-Z][A-Z0-9_]*-\d+)(?:\/([0-9a-f-]+))?$/.exec(url.pathname);
  const labelPath = /^\/api\/labels\/([A-Z][A-Z0-9_]*-\d+)$/.exec(url.pathname);
  const catalogLabelPath = /^\/api\/label-catalog\/(.+)$/.exec(url.pathname);
  if (url.pathname === '/api/status') return json(res, 200, state);
  if (url.pathname === '/api/projects' && req.method === 'GET') {
    try {
      const config = JSON.parse(readFileSync(configPath, 'utf8'));
      return json(res, 200, {projects: await visibleProjects(), selected: workspace.get().projects});
    } catch (error) { return json(res, 503, {projects: [], selected: [], error: error.message}); }
  }
  if (url.pathname === '/api/projects/selection' && req.method === 'PUT') {
    try {
      const value = JSON.parse(await body(req));
      const projects = (value.projects || []).map(project => ({key: String(project.key || '').trim().toUpperCase(), name: String(project.name || project.key || '').trim()})).filter(project => /^[A-Z][A-Z0-9_]*$/.test(project.key));
      if (!projects.length) throw new Error('Select at least one Jira project');
      const saved = workspace.update({projects});
      logger.info('projects.selection_saved', 'Выбранные Jira-проекты сохранены', {projects: saved.projects.map(project => project.key)});
      return json(res, 200, {selected: saved.projects, sync: await syncNow()});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/api/jira-auth/status' && req.method === 'GET') {
    try { return json(res, 200, {...await jiraMcpRequest('/auth/status'), provider: 'embedded-jira'}); }
    catch (error) { return json(res, 503, {configured: false, connected: false, provider: 'embedded-jira', error: error.message}); }
  }
  if (url.pathname === '/api/jira-auth/start' && req.method === 'POST') {
    try {
      const value = JSON.parse(await body(req));
      const current = workspace.update(value.jiraBaseUrl !== undefined ? {jiraBaseUrl: value.jiraBaseUrl} : {});
      return json(res, 200, {...await jiraMcpRequest('/auth/start', {method: 'POST'}), jiraBaseUrl: current.jiraBaseUrl});
    }
    catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/api/workspace' && req.method === 'GET') return json(res, 200, workspace.get());
  if (url.pathname === '/api/workspace' && req.method === 'PUT') {
    try {
      const value = JSON.parse(await body(req));
      return json(res, 200, workspace.update({jiraBaseUrl: value.jiraBaseUrl}));
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/api/llm' && req.method === 'GET') {
    const settings = workspace.get().llm;
    return json(res, 200, {...settings, external:{...settings.external,configured:llmSecrets.configured()}});
  }
  if (url.pathname === '/api/llm' && req.method === 'PUT') {
    try {
      const value=JSON.parse(await body(req));
      const current=workspace.get();
      const llm={...current.llm,...value,external:{...current.llm.external,...(value.external||{})}};
      if(value.external?.apiKey) llmSecrets.save(value.external.apiKey);
      const saved=workspace.update({llm}).llm;
      logger.info('llm.settings_saved','Настройки LLM сохранены',{provider:saved.provider,externalConfigured:llmSecrets.configured()});
      return json(res,200,{...saved,external:{...saved.external,configured:llmSecrets.configured()}});
    } catch(error){ return json(res,400,{error:error.message}); }
  }
  if (url.pathname === '/api/jira-auth/callback' && req.method === 'GET') {
    try {
      await jiraMcpRequest('/auth/callback', {method: 'POST', body: JSON.stringify({code: url.searchParams.get('code'), state: url.searchParams.get('state'), iss: url.searchParams.get('iss')})});
      res.writeHead(302, {...securityHeaders, location: '/?jiraAuth=connected#integration'}); return res.end();
    } catch (error) {
      res.writeHead(302, {...securityHeaders, location: `/?jiraAuth=${encodeURIComponent(error.message)}#integration`}); return res.end();
    }
  }
  if (url.pathname === '/api/jira-auth/disconnect' && req.method === 'DELETE') {
    try { return json(res, 200, await jiraMcpRequest('/auth/disconnect', {method: 'DELETE'})); }
    catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/api/comments' && req.method === 'GET') return json(res, 200, {comments: comments.all()});
  if (url.pathname === '/api/comments/counts' && req.method === 'GET') return json(res, 200, {counts: comments.counts()});
  if (url.pathname === '/api/labels' && req.method === 'GET') return json(res, 200, {labels: labels.all(), catalog: labels.catalog()});
  if (catalogLabelPath && req.method === 'DELETE') {
    try {
      const removed = labels.removeEverywhere(decodeURIComponent(catalogLabelPath[1]));
      logger.info('label.deleted', 'Локальная метка удалена из системы', removed);
      const sync = removed.issues ? await syncNow() : state;
      return json(res, 200, {removed, labels: labels.all(), catalog: labels.catalog(), sync});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (labelPath && req.method === 'GET') return json(res, 200, {issueKey: labelPath[1], labels: labels.list(labelPath[1])});
  if (labelPath && req.method === 'PUT') {
    try {
      const value = JSON.parse(await body(req));
      const taskLabels = labels.set(labelPath[1], value.labels);
      logger.info('labels.updated', 'Локальные метки задачи обновлены', {issueKey: labelPath[1], labels: taskLabels.length, groupingLabel: taskLabels[0] || null});
      const sync = await syncNow();
      return json(res, 200, {issueKey: labelPath[1], labels: taskLabels, sync});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (commentPath && req.method === 'GET' && !commentPath[2]) return json(res, 200, {issueKey: commentPath[1], comments: comments.list(commentPath[1])});
  if (commentPath && req.method === 'POST' && !commentPath[2]) {
    try {
      const value = JSON.parse(await body(req));
      const text = String(value.text || '').trim();
      if (!text) throw new Error('Комментарий не может быть пустым');
      if (text.length > 10_000) throw new Error('Комментарий не должен превышать 10 000 символов');
      const comment = comments.add(commentPath[1], text);
      logger.info('comment.created', 'Добавлен локальный комментарий к задаче', {issueKey: commentPath[1], commentId: comment.id});
      return json(res, 201, {comment});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (commentPath && req.method === 'DELETE') {
    const removed = commentPath[2] ? comments.remove(commentPath[1], commentPath[2]) : comments.removeAll(commentPath[1]);
    if (!removed) return json(res, 404, {error: 'Комментарий не найден'});
    logger.info(commentPath[2] ? 'comment.deleted' : 'comments.deleted', 'Удалён локальный комментарий', {issueKey: commentPath[1], commentId: commentPath[2], removed});
    return json(res, 200, {removed});
  }
  if (url.pathname === '/api/logs') return json(res, 200, {entries: logger.recent(url.searchParams.get('limit'), url.searchParams.get('level') || '')});
  if (url.pathname === '/api/config' && req.method === 'GET') return json(res, 200, JSON.parse(readFileSync(configPath, 'utf8')));
  if (url.pathname === '/api/grouping' && req.method === 'GET') {
    const project = String(url.searchParams.get('project') || '').toUpperCase();
    return json(res, 200, {project, grouping: groupingSettings(workspace.get().groupings[project])});
  }
  if (url.pathname === '/api/grouping' && req.method === 'PUT') {
    try {
      const value = JSON.parse(await body(req));
      const project = String(value.project || '').toUpperCase();
      if (!workspace.get().projects.some(item => item.key === project)) throw new Error('Project is not selected');
      const grouping = groupingSettings(value.grouping);
      const current = workspace.get();
      workspace.update({groupings: {...current.groupings, [project]: grouping}});
      logger.info('grouping.saved', 'Правила группировки проекта сохранены через веб-интерфейс', {project});
      return json(res, 200, {saved: true, grouping, sync: await syncNow()});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  const analyzePath = /^\/api\/projects\/([A-Z][A-Z0-9_]*)\/analyze$/.exec(url.pathname);
  if (analyzePath && req.method === 'POST') {
    try {
      const project = analyzePath[1];
      if (!workspace.get().projects.some(item => item.key === project)) throw new Error('Project is not selected');
      const path = resolveInside(dataDir, `data/${project}-issues.json`);
      if (!existsSync(path)) throw new Error('Synchronize the project before analysis');
      const current = workspace.get();
      const grouping = groupingSettings(current.groupings[project]);
      const issues=JSON.parse(readFileSync(path, 'utf8'));
      const manualThemes=grouping.themes.filter(theme=>theme.source!=='analyzer');
      const llm=new LlmAnalyzer({settings:current.llm,apiKey:llmSecrets.load()});
      const themes=await llm.analyze(current.projects.find(item=>item.key===project),issues);
      const updated = {...grouping, themes: [...manualThemes, ...themes]};
      workspace.update({groupings: {...current.groupings, [project]: updated}});
      logger.info('grouping.analyzed', 'LLM-анализатор пересмотрел правила группировки', {project, provider:current.llm.provider, added:themes.length, issues:issues.length});
      return json(res, 200, {themes, analyzedIssues:issues.length, provider:current.llm.provider, grouping: updated, sync: await syncNow()});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/api/config' && req.method === 'PUT') {
    try {
      const config = JSON.parse(await body(req));
      validateSyncConfig(config);
      writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`);
      logger.info('config.saved', 'Конфигурация сохранена через веб-интерфейс');
      schedule();
      return json(res, 200, {saved: true});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/api/sync' && req.method === 'POST') return json(res, 202, await syncNow());
  if (url.pathname === '/api/document') {
    const config = JSON.parse(readFileSync(configPath, 'utf8'));
    const selected = workspace.get().projects;
    const requested = String(url.searchParams.get('project') || selected[0]?.key || '').toUpperCase();
    if (!selected.some(project => project.key === requested)) return json(res, 404, {error: 'Project is not selected'});
    const mode = url.searchParams.get('mode') === 'sprints' ? 'sprints' : 'backlog';
    const path = resolveInside(dataDir, `data/${requested}-${mode}.md`);
    res.writeHead(existsSync(path) ? 200 : 404, {...securityHeaders, 'content-type': 'text/markdown; charset=utf-8'});
    return res.end(existsSync(path) ? readFileSync(path) : '# Документ ещё не создан\n\nЗапустите синхронизацию.');
  }
  const file = url.pathname === '/' ? 'index.html' : url.pathname.slice(1);
  const path = resolve(publicDir, file);
  if (!path.startsWith(`${publicDir}${sep}`) || !existsSync(path)) { res.writeHead(404, securityHeaders); return res.end('Not found'); }
  const types = {'.html':'text/html; charset=utf-8','.css':'text/css; charset=utf-8','.js':'text/javascript; charset=utf-8'};
  res.writeHead(200, {...securityHeaders, 'content-type': types[extname(path)] || 'application/octet-stream'});
  res.end(readFileSync(path));
});

schedule();
server.listen(port, () => console.log(`Jira MCP Sync UI: http://0.0.0.0:${port}`));
