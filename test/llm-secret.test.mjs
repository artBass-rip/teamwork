import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {LlmSecretStore} from '../src/llm-secret.mjs';

test('external LLM key is encrypted and can be removed',()=>{
  const directory=mkdtempSync(join(tmpdir(),'teamwork-llm-'));
  try {
    const store=new LlmSecretStore(directory);
    store.save('top-secret');
    assert.equal(store.load(),'top-secret');
    assert.equal(readFileSync(join(directory,'external-key.enc'),'utf8').includes('top-secret'),false);
    store.clear();
    assert.equal(store.configured(),false);
  } finally { rmSync(directory,{recursive:true,force:true}); }
});
