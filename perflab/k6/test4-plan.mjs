import fs from 'node:fs';
import { createSequence, userIndexFor } from './sequence.mjs';
import { createOperation, routeTemplates } from './requests.mjs';
import { diagnosticSequence } from './test4-sequence.mjs';

if (process.argv.includes('--routes')) {
  process.stdout.write(JSON.stringify(routeTemplates.map((entry, index) => ({ selector: `route-${index}`, route: entry[2] })), null, 2) + '\n');
} else {
  const config = JSON.parse(fs.readFileSync(0, 'utf8'));
  const corpus = JSON.parse(fs.readFileSync(config.corpus_path, 'utf8'));
  const credentials = JSON.parse(fs.readFileSync(config.credentials_path, 'utf8'));
  const users = Math.min(config.virtual_users, credentials.users.length);
  if (!users) throw new Error('账号集合为空');
  for (let index = 0; index < users; index += 1) {
    if (credentials.users[index].username !== corpus.users[index].username) throw new Error('凭据与语料的账号顺序不一致');
  }
  const operation = createOperation({ readPostIDs: corpus.read_post_ids, interactionPostIDs: corpus.interaction_post_ids,
    editablePostIDs: corpus.editable_post_ids, deletePostIDs: corpus.delete_post_ids, corpusUsers: corpus.users,
    credentialUsers: credentials.users, credentialPassword: credentials.password, profile: 'smooth-mixed' });
  const sequence = config.route_set === 'mixed' ? createSequence(config.mix) : diagnosticSequence(config.route_set);
  const counts = { warmup: {}, measurement: {} };
  const deleted = new Set();
  const total = Math.ceil(config.rps * (config.warmup_seconds + config.measurement_seconds));
  for (let slot = 0; slot < total; slot += 1) {
    const selection = sequence(slot);
    const request = operation(slot, userIndexFor(selection, users), selection, users);
    const phase = slot < Math.ceil(config.rps * config.warmup_seconds) ? 'warmup' : 'measurement';
    counts[phase][request.route] = (counts[phase][request.route] || 0) + 1;
    if (request.route === 'DELETE /api/v1/posts/:postId') {
      if (deleted.has(request.path)) throw new Error('完整请求序列存在重复删除目标');
      deleted.add(request.path);
    }
  }
  process.stdout.write(JSON.stringify({ business_requests: total, expected_routes: counts, unique_delete_targets: deleted.size, sessions: users }) + '\n');
}
