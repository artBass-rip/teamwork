import {randomBytes} from 'node:crypto';
import {auth} from '@modelcontextprotocol/client';

export class RovoOAuthProvider {
  constructor({redirectUri, store}) {
    this.redirectUri = redirectUri;
    this.store = store;
    this.authorizationUrl = null;
    const current = store.load() || {};
    const allowed = Object.fromEntries(['state', 'clientInformation', 'tokens', 'codeVerifier', 'discoveryState'].filter(key => current[key] != null).map(key => [key, current[key]]));
    if (Object.keys(current).some(key => !(key in allowed))) store.save(allowed);
  }

  snapshot() { return this.store.load() || {}; }
  update(values) { this.store.save({...this.snapshot(), ...values}); }
  get redirectUrl() { return this.redirectUri; }
  get clientMetadata() {
    return {
      client_name: 'TeamWork Jira document sync',
      redirect_uris: [this.redirectUri],
      grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'],
      token_endpoint_auth_method: 'none'
    };
  }
  state() {
    const state = randomBytes(24).toString('hex');
    this.update({state});
    return state;
  }
  clientInformation() { return this.snapshot().clientInformation; }
  saveClientInformation(clientInformation) { this.update({clientInformation}); }
  tokens() { return this.snapshot().tokens; }
  saveTokens(tokens) { this.update({tokens}); }
  redirectToAuthorization(url) { this.authorizationUrl = String(url); }
  consumeAuthorizationUrl() { const value = this.authorizationUrl; this.authorizationUrl = null; return value; }
  saveCodeVerifier(codeVerifier) { this.update({codeVerifier}); }
  codeVerifier() { return this.snapshot().codeVerifier || ''; }
  saveDiscoveryState(discoveryState) { this.update({discoveryState}); }
  discoveryState() { return this.snapshot().discoveryState; }
  expectedIssuer() {
    const discovery = this.discoveryState();
    return discovery?.authorizationServerMetadata?.issuer || discovery?.authorizationServerUrl;
  }
  invalidateCredentials(scope) {
    if (scope === 'all') return this.store.clear();
    const current = this.snapshot();
    if (scope === 'client') delete current.clientInformation;
    if (scope === 'tokens') delete current.tokens;
    if (scope === 'verifier') delete current.codeVerifier;
    if (scope === 'discovery') delete current.discoveryState;
    this.store.save(current);
  }
  validateState(state) {
    const expected = this.snapshot().state;
    if (!expected || expected !== state) throw new Error('OAuth state is invalid or expired');
    this.update({state: null});
  }
  status() {
    const saved = this.snapshot();
    return {configured: true, connected: Boolean(saved.tokens?.access_token), authentication: 'browser-oauth-2.1', expiresAt: saved.tokens?.expires_at || null};
  }
  disconnect() { this.store.clear(); }
}

export class RovoOAuth {
  constructor({serverUrl, provider, fetchImpl = fetch}) {
    this.serverUrl = serverUrl;
    this.provider = provider;
    this.fetch = fetchImpl;
    this.scope = 'read:me read:account email offline_access read:jira-work read:all:twg';
  }
  async startUrl() {
    const result = await auth(this.provider, {serverUrl: this.serverUrl, scope: this.scope, fetchFn: this.fetch});
    if (result === 'AUTHORIZED') return null;
    const url = this.provider.consumeAuthorizationUrl();
    if (!url) throw new Error('Atlassian did not provide an authorization URL');
    return url;
  }
  async complete(code, state, iss) {
    this.provider.validateState(state);
    const callbackIssuer = iss || this.provider.expectedIssuer();
    await auth(this.provider, {serverUrl: this.serverUrl, authorizationCode: code, iss: callbackIssuer, scope: this.scope, fetchFn: this.fetch});
    return this.provider.status();
  }
  status() { return this.provider.status(); }
  disconnect() { this.provider.disconnect(); }
}
