import test from 'node:test';
import assert from 'node:assert/strict';
import {groupMarkdownBySprint} from '../public/sprints.js';

test('sprint view reverses hierarchy and keeps placement order', () => {
  const source = `# Tasks

## Goal: No Goal (3)

### Platform (2)

#### Backlog (1)

##### ☑ [DO-2](https://jira/browse/DO-2) — Backlog task — To Do

- Status: To Do

#### Active sprint — Sprint 8 (1)

##### ☑ [DO-1](https://jira/browse/DO-1) — Active task — In progress

### Security (1)

#### Future sprint — Sprint 9 (1)

##### ☑ [DO-3](https://jira/browse/DO-3) — Future task — To Do
`;
  const config = {grouping: {placementLabels: {active: 'Active sprint', future: 'Future sprint', backlog: 'Backlog'}}};
  const result = groupMarkdownBySprint(source, config);
  assert.ok(result.indexOf('## Active sprint — Sprint 8 (1)') < result.indexOf('## Future sprint — Sprint 9 (1)'));
  assert.ok(result.indexOf('## Future sprint — Sprint 9 (1)') < result.indexOf('## Backlog (1)'));
  assert.match(result, /### Goal: No Goal \(1\)[\s\S]*#### Platform \(1\)[\s\S]*DO-1/);
});
