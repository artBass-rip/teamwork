import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {WorkspaceStore, normalizeBaseUrl} from '../src/workspace.mjs';

test('selected projects and Jira URL persist outside grouping configuration', () => {
  const directory = mkdtempSync(join(tmpdir(), 'teamwork-workspace-'));
  try {
    const path = join(directory, 'workspace.json');
    new WorkspaceStore(path).update({projects: [{key: 'do', name: 'DevOps'}], jiraBaseUrl: 'https://example.atlassian.net/path'});
    const saved=new WorkspaceStore(path).get();
    assert.deepEqual(saved.projects,[{key:'DO',name:'DevOps'}]);
    assert.equal(saved.jiraBaseUrl,'https://example.atlassian.net');
    assert.equal(saved.llm.provider,'ollama');
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }
});

test('Jira URL is optional but rejects insecure origins', () => {
  assert.equal(normalizeBaseUrl(''), '');
  assert.throws(() => normalizeBaseUrl('http://example.atlassian.net'), /HTTPS/);
});
