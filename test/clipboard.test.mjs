import test from 'node:test';
import assert from 'node:assert/strict';
import {plainTaskText} from '../public/clipboard.js';

test('plain clipboard representation preserves the Jira URL', () => {
  assert.equal(
    plainTaskText('DO-42 — Certificate rotation — In progress', 'https://example.atlassian.net/browse/DO-42', 'Исполнитель: Artem'),
    'DO-42 — Certificate rotation — In progress\n\nhttps://example.atlassian.net/browse/DO-42\n\nИсполнитель: Artem'
  );
});
