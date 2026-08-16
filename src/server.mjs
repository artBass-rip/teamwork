import http from 'node:http';
import {readFileSync, writeFileSync, existsSync} from 'node:fs';
import {extname, resolve, sep} from 'node:path';
import {synchronize} from './sync.mjs';
import {Logger} from './logger.mjs';
import {CommentStore} from './comments.mjs';
import {LabelStore} from './labels.mjs';
import {isAuthorized, resolveInside, securityHeaders} from './security.mjs';

const port = Number(process.env.PORT || 8080);
const configPath = resolve(process.env.CONFIG_PATH || 'grouping.config.json');
const mcpUrl = process.env.MCP_URL || 'http://gateway:8080/mcp';
const mcpAuthToken = process.env.MCP_AUTH_TOKEN || '';
const publicDir = resolve('public');
const dataDir = resolve(process.env.DATA_DIR || 'data');
const authUser = process.env.APP_AUTH_USER || 'admin';
const authPassword = process.env.APP_AUTH_PASSWORD || '';
const logger = new Logger(resolve(process.env.LOG_PATH || 'data/app.log'));
const comments = new CommentStore(resolve(process.env.COMMENTS_PATH || 'data/comments.json'), logger);
const labels = new LabelStore(resolve(process.env.LABELS_PATH || 'data/labels.json'), logger);
let state = {running: false, lastSuccess: null, lastError: null, issues: null};
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
    const result = await synchronize(configPath, mcpUrl, mcpAuthToken, dataDir, labels.all(), (event, message, context = {}) => logger.info(event, message, {runId, ...context}));
    const pruned = comments.prune(result.issueKeys);
    if (pruned.length) logger.info('comments.pruned', 'Удалены комментарии отсутствующих в документе задач', {runId, issues: pruned.length, comments: pruned.reduce((sum, item) => sum + item.comments, 0)});
    const prunedLabels = labels.prune(result.issueKeys);
    if (prunedLabels.length) logger.info('labels.pruned', 'Удалены метки отсутствующих в документе задач', {runId, issues: prunedLabels.length});
    state = {...state, running: false, lastSuccess: result.updatedAt, issues: result.issues, output: result.output};
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
  if (url.pathname === '/api/config' && req.method === 'PUT') {
    try {
      const config = JSON.parse(await body(req));
      if (!config.schedule || !config.jira || !config.document || !config.grouping) throw new Error('Отсутствуют обязательные секции конфигурации');
      resolveInside(dataDir, config.document.outputPath);
      writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`);
      logger.info('config.saved', 'Конфигурация сохранена через веб-интерфейс');
      schedule();
      return json(res, 200, {saved: true});
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/api/sync' && req.method === 'POST') return json(res, 202, await syncNow());
  if (url.pathname === '/api/document') {
    const config = JSON.parse(readFileSync(configPath, 'utf8'));
    const path = resolveInside(dataDir, config.document.outputPath);
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
