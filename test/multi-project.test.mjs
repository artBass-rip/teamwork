import test from 'node:test';
import assert from 'node:assert/strict';
import {documentForProject, groupingSettings, jqlForProject, projectsForConfig, renderBacklog, validateSyncConfig} from '../src/sync.mjs';

test('project JQL template is resolved independently for every selected project', () => {
  assert.equal(
    jqlForProject({jqlTemplate: 'project="{project}" AND statusCategory != Done'}, 'DO'),
    'project="DO" AND statusCategory != Done'
  );
});

test('legacy single-project JQL remains compatible', () => {
  assert.equal(
    jqlForProject({jql: 'project = OLD AND resolution = Unresolved'}, 'NEW'),
    'project="NEW" AND resolution = Unresolved'
  );
});

test('invalid project entries produce a useful configuration error', () => {
  assert.throws(() => projectsForConfig({projects: [{name: 'Missing key'}]}), /No valid Jira projects/);
  assert.throws(() => jqlForProject({jqlTemplate: 'project="{project}"'}, undefined), /key is missing or invalid/);
});

test('an incomplete configuration is rejected before synchronization', () => {
  assert.throws(() => validateSyncConfig({schedule: {}}), /Configuration is incomplete/);
});

test('document settings are derived from each selected project', () => {
  const document = documentForProject({key: 'DO', name: 'DevOps'});
  assert.equal(document.title, 'DevOps — бэклог и созданные спринты');
  assert.equal(document.outputPath, 'data/DO-jira-backlog-and-sprints.md');
});

test('grouping defaults work without a grouping configuration section', () => {
  const grouping = groupingSettings();
  assert.deepEqual(grouping.themes, []);
  assert.equal(grouping.fallbackTheme, 'Прочее');
});

test('Backlog is flat and never contains sprint grouping by default', () => {
  const config = {jira:{baseUrl:'https://jira.example'},document:documentForProject({key:'DO',name:'DevOps'}),grouping:groupingSettings()};
  const markdown = renderBacklog([{key:'DO-1',title:'Task',status:'Open',type:'Task',assignee:'Nobody',labels:[],components:[],sprints:[{state:'active',name:'Sprint 1'}]}], config);
  assert.match(markdown, /## Задачи \(1\)/);
  assert.doesNotMatch(markdown, /Активный спринт|Sprint 1/);
});
