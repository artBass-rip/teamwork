import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {LabelStore, normalizeLabels} from '../src/labels.mjs';
import {groupFor, groupsFor, themeFor} from '../src/sync.mjs';

test('labels are normalized, deduplicated, persisted, and pruned', () => {
  assert.deepEqual(normalizeLabels([' Security ', 'security', 'Needs review']), ['Security', 'Needs review']);
  const directory = mkdtempSync(join(tmpdir(), 'teamwork-labels-'));
  try {
    const store = new LabelStore(join(directory, 'labels.json'), {error() {}});
    assert.deepEqual(store.set('DO-1', ['Platform', 'Urgent']), ['Platform', 'Urgent']);
    store.set('DO-2', ['Security']);
    assert.deepEqual(store.list('DO-1'), ['Platform', 'Urgent']);
    assert.deepEqual(store.catalog(), [{name: 'Platform', issues: 1}, {name: 'Security', issues: 1}, {name: 'Urgent', issues: 1}]);
    assert.deepEqual(store.removeEverywhere('security'), {label: 'security', issues: 1});
    assert.deepEqual(store.list('DO-2'), []);
    assert.deepEqual(store.prune(['DO-1']), []);
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }
});

test('first local label overrides theme grouping', () => {
  const issue = {key: 'DO-1', title: 'Terraform module', description: '', labels: [], components: []};
  const config = {grouping: {labelGroupPrefix: 'Label', themes: [{name: 'IaC', pattern: 'terraform'}], fallbackTheme: 'Other'}};
  assert.equal(groupFor(issue, config, {}), 'IaC');
  assert.equal(groupFor(issue, config, {'DO-1': ['Priority', 'Secondary']}), 'Label: Priority');
  assert.deepEqual(groupsFor(issue, config, {'DO-1': ['Priority', 'Secondary']}), ['Label: Priority', 'Label: Secondary']);
});

test('weighted classification prioritizes title and Jira metadata over description', () => {
  const config = {grouping: {
    sourceWeights: {title: 8, labels: 7, components: 7, description: 1},
    themes: [
      {name: 'Delivery', pattern: '\\b(gitlab|pipeline|build)\\b'},
      {name: 'Security', pattern: '\\b(iam|role|policy|certificate)\\b'},
      {name: 'Data', pattern: '\\b(kafka|mongo|postgres)\\b'}
    ],
    fallbackTheme: 'Other'
  }};
  assert.equal(themeFor({title: 'Client certificate support', description: 'Build pipeline pipeline', labels: [], components: []}, config), 'Security');
  assert.equal(themeFor({title: 'Generic maintenance', description: '', labels: ['kafka'], components: []}, config), 'Data');
  assert.equal(themeFor({title: 'Unclassified work', description: '', labels: [], components: []}, config), 'Other');
});
