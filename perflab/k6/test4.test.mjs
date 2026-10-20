import test from 'node:test';
import assert from 'node:assert/strict';
import { diagnosticSequence } from './test4-sequence.mjs';
import { createOperation } from './requests.mjs';
import { userIndexFor } from './sequence.mjs';

test('诊断路由保持固定分布，写诊断各占 50%', () => {
  assert.equal(diagnosticSequence('feed')(500).route, 2);
  assert.equal(diagnosticSequence('detail')(500).route, 1);
  assert.throws(() => diagnosticSequence('unknown'));
  const sequence = diagnosticSequence('write');
  const counts = {};
  for (let slot = 0; slot < 10000; slot += 1) {
    const entry = sequence(slot);
    const key = `${entry.category}:${entry.route}`;
    counts[key] = (counts[key] || 0) + 1;
  }
  for (let route = 0; route < 4; route += 1) assert.equal(counts[`content_write:${route}`], 1250);
  for (let route = 0; route < 5; route += 1) assert.equal(counts[`interaction_write:${route}`], 1000);
});

test('写诊断删除目标不重复，语料用尽时中止', () => {
  const users = 7;
  const operation = createOperation({ readPostIDs: [1], interactionPostIDs: [2],
    editablePostIDs: Array.from({ length: users }, (_, user) => [10 + user]),
    deletePostIDs: Array.from({ length: users }, (_, user) => [100 + user, 200 + user]),
    corpusUsers: Array.from({ length: users }, (_, id) => ({ id, username: `u${id}` })),
    credentialUsers: [], credentialPassword: '', profile: 'smooth-mixed' });
  const sequence = diagnosticSequence('write');
  const paths = new Set();
  let exhausted = false;
  for (let slot = 0; slot < 1000; slot += 1) {
    const entry = sequence(slot);
    if (entry.category !== 'content_write' || entry.route !== 3) continue;
    try {
      const request = operation(slot, userIndexFor(entry, users), entry, users);
      assert(!paths.has(request.path)); paths.add(request.path);
    } catch (error) {
      assert.match(error.message, /语料已耗尽/); exhausted = true; break;
    }
  }
  assert.equal(paths.size, 14); assert(exhausted);
});
