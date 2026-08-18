import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync, mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {EncryptedTokenStore, persistentEncryptionSecret} from '../mcp/token-store.mjs';
import {RovoOAuthProvider} from '../mcp/oauth.mjs';

test('Jira OAuth tokens are encrypted at rest and can be removed', () => {
  const directory = mkdtempSync(join(tmpdir(), 'teamwork-jira-token-'));
  try {
    const store = new EncryptedTokenStore(join(directory, 'oauth.enc'), 'a-secure-test-key-that-is-longer-than-32-characters');
    const token = {access_token: 'access-secret', refresh_token: 'refresh-secret', expires_at: 123};
    store.save(token);
    assert.deepEqual(store.load(), token);
    store.clear();
    assert.equal(store.load(), null);
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }
});

test('Atlassian browser OAuth provider persists PKCE state and tokens', () => {
  let saved;
  const store = {load: () => saved, save: value => { saved = value; }, clear: () => { saved = undefined; }};
  const provider = new RovoOAuthProvider({redirectUri: 'http://localhost/callback', store});
  const state = provider.state();
  provider.saveCodeVerifier('pkce-verifier');
  provider.saveClientInformation({client_id: 'dynamic-client'});
  provider.saveDiscoveryState({authorizationServerUrl: 'https://auth.example.test/issuer', authorizationServerMetadata: {issuer: 'https://auth.example.test/issuer'}});
  provider.saveTokens({access_token: 'access', refresh_token: 'refresh', token_type: 'bearer'});
  assert.equal(provider.codeVerifier(), 'pkce-verifier');
  assert.equal(provider.clientInformation().client_id, 'dynamic-client');
  assert.equal(provider.status().connected, true);
  assert.equal(provider.status().authentication, 'browser-oauth-2.1');
  assert.equal(provider.expectedIssuer(), 'https://auth.example.test/issuer');
  provider.validateState(state);
  assert.throws(() => provider.validateState(state), /invalid or expired/);
  provider.disconnect();
  assert.equal(provider.status().connected, false);
});

test('encryption key persists beside OAuth data across container recreation', () => {
  const directory = mkdtempSync(join(tmpdir(), 'teamwork-jira-key-'));
  try {
    const path = join(directory, 'encryption.key');
    const first = persistentEncryptionSecret(path, 'migration-key-that-is-longer-than-thirty-two-characters');
    const second = persistentEncryptionSecret(path, 'a-different-key-that-must-never-replace-the-first-one');
    assert.equal(second, first);
    assert.equal(existsSync(path), true);
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }
});

test('an unreadable OAuth envelope is quarantined instead of crashing the MCP server', () => {
  const directory = mkdtempSync(join(tmpdir(), 'teamwork-jira-quarantine-'));
  try {
    const path = join(directory, 'oauth.enc');
    new EncryptedTokenStore(path, 'first-encryption-key-that-is-long-enough').save({access_token: 'secret'});
    const replacement = new EncryptedTokenStore(path, 'different-encryption-key-that-is-long-enough');
    assert.equal(replacement.load(), null);
    assert.equal(existsSync(path), false);
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }
});
