import http from 'node:http';
import {timingSafeEqual} from 'node:crypto';
import {McpServer} from '@modelcontextprotocol/server';
import {NodeStreamableHTTPServerTransport} from '@modelcontextprotocol/node';
import {z} from 'zod';
import {EncryptedTokenStore, persistentEncryptionSecret} from './token-store.mjs';
import {RovoOAuthProvider, RovoOAuth} from './oauth.mjs';
import {JiraClient} from './jira-client.mjs';

const port = Number(process.env.PORT || 8081);
const internalToken = process.env.MCP_AUTH_TOKEN || '';
const allowedOrigins = new Set(String(process.env.ALLOWED_ORIGINS || 'http://localhost:8080,http://127.0.0.1:8080').split(',').map(value => value.trim()).filter(Boolean));
const credentialPath = process.env.CREDENTIAL_STORE_PATH || '/var/lib/teamwork/atlassian-oauth.enc';
const encryptionSecret = persistentEncryptionSecret(process.env.ENCRYPTION_KEY_PATH || '/var/lib/teamwork/encryption.key', process.env.JIRA_TOKEN_ENCRYPTION_KEY || '');
const store = new EncryptedTokenStore(credentialPath, encryptionSecret);
const rovoUrl = process.env.ATLASSIAN_MCP_URL || 'https://mcp.atlassian.com/v1/mcp/authv2';
const oauthProvider = new RovoOAuthProvider({redirectUri: process.env.ATLASSIAN_REDIRECT_URI || 'http://localhost:8080/api/jira-auth/callback', store});
const oauth = new RovoOAuth({serverUrl: rovoUrl, provider: oauthProvider});
const jira = new JiraClient(oauthProvider, {serverUrl: rovoUrl});

function secureEqual(actual, expected) {
  const left = Buffer.from(actual || ''); const right = Buffer.from(expected || '');
  return left.length === right.length && timingSafeEqual(left, right);
}

function authorized(req) { return internalToken && secureEqual(req.headers.authorization, `Bearer ${internalToken}`); }
const json = (res, status, value) => { res.writeHead(status, {'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store'}); res.end(JSON.stringify(value)); };
const toolResult = value => ({content: [{type: 'text', text: JSON.stringify(value)}], structuredContent: value});
async function requestBody(req, limit = 16_384) {
  let body = '';
  for await (const chunk of req) {
    body += chunk;
    if (body.length > limit) throw new Error('Request body is too large');
  }
  return JSON.parse(body || '{}');
}

function createMcpServer() {
  const server = new McpServer({name: 'teamwork-jira-mcp', version: '0.1.0'}, {capabilities: {tools: {}}});
  server.registerTool('jira_export_snapshot', {title: 'Export Jira snapshot', description: 'Read and normalize all Jira issues matching a JQL query.', inputSchema: z.object({cloudId: z.string().default(''), jql: z.string().min(1), goalFieldName: z.string().default('Goal'), sprintFieldId: z.string().default('customfield_10020')})}, async args => toolResult({issues: await jira.exportSnapshot(args)}));
  server.registerTool('jira_search_issues', {title: 'Search Jira issues', description: 'Run a read-only JQL search.', inputSchema: z.object({cloudId: z.string().default(''), jql: z.string().min(1), fields: z.array(z.string()).default(['summary']), maxResults: z.number().int().min(1).max(100).default(100)})}, async args => toolResult({issues: await jira.search(args)}));
  server.registerTool('jira_get_issue', {title: 'Get Jira issue', description: 'Read a single Jira issue.', inputSchema: z.object({cloudId: z.string().default(''), key: z.string().regex(/^[A-Z][A-Z0-9_]*-\d+$/)})}, async ({key, cloudId}) => toolResult(await jira.issue(key, cloudId)));
  server.registerTool('jira_list_projects', {title: 'List Jira projects', description: 'List Jira projects visible to the authenticated user.', inputSchema: z.object({cloudId: z.string().default('')})}, async ({cloudId}) => toolResult({projects: await jira.projects(cloudId)}));
  server.registerTool('jira_list_sites', {title: 'List Atlassian sites', description: 'List Atlassian sites accessible to the authenticated user.', inputSchema: z.object({})}, async () => toolResult({sites: await jira.sites()}));
  return server;
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`);
  if (url.pathname === '/health') return json(res, 200, {status: 'ok', auth: oauth.status()});
  if (!authorized(req)) return json(res, 401, {error: 'Unauthorized'});
  if (url.pathname === '/auth/status' && req.method === 'GET') return json(res, 200, oauth.status());
  if (url.pathname === '/auth/start' && req.method === 'POST') {
    try { return json(res, 200, {url: await oauth.startUrl()}); } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/auth/callback' && req.method === 'POST') {
    try {
      const value = await requestBody(req);
      return json(res, 200, await oauth.complete(value.code, value.state, value.iss));
    } catch (error) { return json(res, 400, {error: error.message}); }
  }
  if (url.pathname === '/auth/disconnect' && req.method === 'DELETE') { oauth.disconnect(); return json(res, 200, {connected: false}); }
  if (url.pathname !== '/mcp') return json(res, 404, {error: 'Not found'});
  const origin = req.headers.origin;
  if (origin && !allowedOrigins.has(origin)) return json(res, 403, {error: 'Origin is not allowed'});
  try {
    const mcp = createMcpServer();
    const transport = new NodeStreamableHTTPServerTransport({sessionIdGenerator: undefined, enableJsonResponse: true});
    await mcp.connect(transport);
    await transport.handleRequest(req, res);
  } catch (error) {
    if (!res.headersSent) json(res, 500, {error: error.message});
  }
});

server.listen(port, '0.0.0.0', () => console.log(`TeamWork Jira MCP listening on ${port}`));
