import http from 'k6/http';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import { createOperation, routeTemplates } from '../k6/requests.mjs';
import { createSequence, userIndexFor } from '../k6/sequence.mjs';

const vus = 512;
const rps = Number(__ENV.LOAD_TARGET_RPS);
const warmup = Number(__ENV.LOAD_WARMUP_SECONDS);
const measurement = Number(__ENV.LOAD_MEASUREMENT_SECONDS);
const offset = Number(__ENV.LOAD_SLOT_OFFSET || 0);
const routeIndex = Number(__ENV.LOAD_ROUTE || -1);
const base = __ENV.GOPULSE_BASE_URL;
function field(name) { return new SharedArray(`pt_${name}`, () => JSON.parse(open('/input/corpus.json'))[name]); }
const corpusUsers = field('users');
const credentialUsers = new SharedArray('pt_credentials', () => JSON.parse(open('/input/credentials.json')).users);
const password = new SharedArray('pt_password', () => [JSON.parse(open('/input/credentials.json')).password])[0];
const sequence = createSequence(JSON.parse(__ENV.LOAD_MIX_JSON));
const operation = createOperation({ readPostIDs: field('read_post_ids'), interactionPostIDs: field('interaction_post_ids'),
  editablePostIDs: field('editable_post_ids'), deletePostIDs: field('delete_post_ids'), corpusUsers, credentialUsers,
  credentialPassword: password, profile: 'smooth-mixed', strictDelete: true, vu: () => __VU });
export const options = {
  scenarios: { business: { executor: 'constant-arrival-rate', rate: rps, timeUnit: '1s',
    duration: `${warmup + measurement}s`, preAllocatedVUs: vus, maxVUs: vus, gracefulStop: '5s' } },
  setupTimeout: '300s', discardResponseBodies: false, summaryTrendStats: ['avg', 'p(95)', 'p(99)'],
};
function emit(event) { console.log(`PT ${JSON.stringify({ schema_version: 'gopulse.pt.request.v1', ...event })}`); }
export function setup() {
  const sessions = [];
  for (let index = 0; index < vus; index += 1) {
    const response = http.post(`${base}/api/v1/auth/login`, JSON.stringify({ username: credentialUsers[index].username, password }),
      { timeout: '5s', headers: { 'Content-Type': 'application/json' }, tags: { phase: 'setup' } });
    const cookie = response.cookies.gopulse_session && response.cookies.gopulse_session[0];
    if (response.status !== 200 || !cookie || !cookie.value) {
      emit({ type: 'fatal', phase: 'setup', error: 'login_failed' });
      exec.test.abort('登录准备失败');
    }
    sessions.push(cookie.value);
  }
  return { sessions };
}
export default function (data) {
  const scenario = exec.scenario.startTime;
  const slot = offset + exec.scenario.iterationInTest;
  const phase = Date.now() - scenario < warmup * 1000 ? 'warmup' : 'measurement';
  let selection = sequence(slot);
  if (routeIndex >= 0) {
    const target = routeTemplates[routeIndex];
    const categoryRoutes = routeTemplates.filter((item) => item[0] === target[0]);
    selection = { category: target[0], route: categoryRoutes.indexOf(target), ordinal: slot, routeVisit: slot };
  }
  const actor = userIndexFor(selection, data.sessions.length);
  let request;
  try { request = operation(slot, actor, selection, data.sessions.length); }
  catch (_) { emit({ type: 'fatal', error: 'corpus_exhausted', phase }); exec.test.abort('语料已耗尽'); return; }
  const common = { request_id: `${__ENV.LOAD_STAGE_ID}:${slot}`, route: request.route, category: request.category,
    phase, started_at_ms: Date.now(), scenario_started_at_ms: scenario };
  const sample = { path: request.path, actor };
  if (request.category === 'content_write' && request.body) sample.expected = JSON.parse(request.body);
  emit({ type: 'start', ...common, sample });
  const headers = { Cookie: `gopulse_session=${data.sessions[actor]}` };
  if (request.body !== undefined) headers['Content-Type'] = 'application/json';
  const response = http.request(request.method, base + request.path, request.body,
    { headers, timeout: '5s', tags: { phase, name: request.route } });
  let body;
  try { body = response.json(); } catch (_) { body = null; }
  const success = request.statuses.includes(response.status) && response.status !== 404
    && (!body || body.code === undefined || body.code === 0);
  sample.response_id = body && body.data && body.data.id;
  emit({ type: 'outcome', ...common, completed_at_ms: Date.now(), success,
    http_ms: response.timings.duration, status: response.status, error: response.error || '', sample });
  if (request.login && response.cookies.gopulse_session && response.cookies.gopulse_session[0]) {
    data.sessions[actor] = response.cookies.gopulse_session[0].value;
  }
}
export function handleSummary(data) {
  delete data.setup_data;
  return { '/output/summary.json': JSON.stringify(data) };
}
