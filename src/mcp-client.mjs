export class McpClient {
  constructor(url, authToken = '') { this.url = url; this.authToken = authToken; this.sessionId = null; this.id = 1; }

  async request(method, params = {}) {
    const headers = {
      'content-type': 'application/json',
      accept: 'application/json, text/event-stream'
    };
    if (this.authToken) headers.authorization = `Bearer ${this.authToken}`;
    if (this.sessionId) headers['mcp-session-id'] = this.sessionId;
    let response;
    try {
      response = await fetch(this.url, {
        method: 'POST', headers,
        body: JSON.stringify({jsonrpc: '2.0', id: this.id++, method, params})
      });
    } catch (error) {
      const cause = error.cause?.message || error.cause?.code || error.message;
      throw new Error(`Встроенный Jira MCP недоступен по адресу ${this.url}: ${cause}`);
    }
    if (!response.ok) throw new Error(`MCP HTTP ${response.status}: ${await response.text()}`);
    this.sessionId ||= response.headers.get('mcp-session-id');
    const body = await response.text();
    const payloads = response.headers.get('content-type')?.includes('text/event-stream')
      ? body.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trim())
      : [body];
    const message = payloads.filter(Boolean).map(JSON.parse).find(item => item.id != null);
    if (!message) throw new Error(`MCP returned no JSON-RPC response: ${body.slice(0, 500)}`);
    if (message.error) throw new Error(JSON.stringify(message.error));
    return message.result;
  }

  async initialize() {
    await this.request('initialize', {
      protocolVersion: '2025-03-26', capabilities: {},
      clientInfo: {name: 'jira-do-sync', version: '1.0.0'}
    });
  }
}
