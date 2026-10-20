import http from 'k6/http';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import { Trend, Counter } from 'k6/metrics';
import { createSequence, userIndexFor, isEffectiveSuccess } from './sequence.mjs';
import { createOperation } from './requests.mjs';
import { diagnosticSequence } from './test4-sequence.mjs';

const baseURL = __ENV.GOPULSE_BASE_URL;
const runID = __ENV.LOAD_RUN_ID;
const rps = Number(__ENV.LOAD_TARGET_RPS);
const warmup = Number(__ENV.LOAD_WARMUP_SECONDS || 30);
const duration = Number(__ENV.LOAD_MEASUREMENT_SECONDS || 300);
const scenarioTail = Number(__ENV.LOAD_SCENARIO_TAIL_SECONDS || 60);
const measuredDuration = warmup + duration;
const vus = Number(__ENV.LOAD_VUS || 512);
const timeout = Number(__ENV.LOAD_REQUEST_TIMEOUT_SECONDS || 5);
const cookieName = 'gopulse_session';
const mix = JSON.parse(__ENV.LOAD_MIX_JSON);
const mode = __ENV.LOAD_ROUTE_SET || 'mixed';
const sequence = mode === 'mixed' ? createSequence(mix) : diagnosticSequence(mode);
function field(name) {
  return new SharedArray(`pt4_${name}`, () => JSON.parse(open(__ENV.LOAD_CORPUS_PATH))[name]);
}
const corpus = {
  readPostIDs: field('read_post_ids'), interactionPostIDs: field('interaction_post_ids'),
  editablePostIDs: field('editable_post_ids'), deletePostIDs: field('delete_post_ids'), corpusUsers: field('users'),
  credentialUsers: new SharedArray('pt4_credentials', () => JSON.parse(open(__ENV.LOAD_CREDENTIALS_PATH)).users),
  credentialPassword: new SharedArray('pt4_password', () => [JSON.parse(open(__ENV.LOAD_CREDENTIALS_PATH)).password])[0],
};
const operation = createOperation({ ...corpus, profile: 'smooth-mixed', vu: () => __VU });
const latency = new Trend('pt4_http_latency', true);
const endToEnd = new Trend('pt4_end_to_end', true);
const started = new Counter('pt4_started');
const completed = new Counter('pt4_completed');

export const options = {
  scenarios: {
    business: { executor: 'constant-arrival-rate', rate: Math.round(rps * 100), timeUnit: '100s', duration: `${measuredDuration + scenarioTail}s`,
      preAllocatedVUs: vus, maxVUs: vus, exec: 'business', gracefulStop: `${timeout}s` },
    health: { executor: 'constant-arrival-rate', rate: 1, timeUnit: '1s', duration: `${measuredDuration + scenarioTail}s`,
      preAllocatedVUs: 1, maxVUs: 1, exec: 'health', gracefulStop: `${timeout}s` },
  },
  setupTimeout: '300s', systemTags: ['status', 'method', 'scenario', 'error_code', 'expected_response'],
  discardResponseBodies: false, summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)'],
};

function emit(event) { console.log(`PT4 ${JSON.stringify({ run_id: runID, ...event })}`); }

export function setup() {
  const sessions = [];
  const count = Math.min(vus, corpus.credentialUsers.length);
  for (let start = 0; start < count; start += 16) {
    const batch = [];
    for (let index = start; index < Math.min(count, start + 16); index += 1) {
      batch.push({ method: 'POST', url: `${baseURL}/api/v1/auth/login`,
        body: JSON.stringify({ username: corpus.credentialUsers[index].username, password: corpus.credentialPassword }),
        params: { headers: { 'Content-Type': 'application/json' }, timeout: `${timeout}s`, tags: { phase: 'setup', route: 'POST /api/v1/auth/login' } } });
    }
    for (const response of http.batch(batch)) {
      const cookie = response.cookies[cookieName] && response.cookies[cookieName][0];
      if (response.status !== 200 || !cookie || !cookie.value) throw new Error('测试四 setup 登录失败');
      sessions.push(cookie.value);
    }
  }
  if (!sessions.length) throw new Error('测试四凭据为空');
  return { sessions };
}

export function business(data) {
  const slot = exec.scenario.iterationInTest;
  const begin = Date.now();
  const elapsed = (begin - exec.scenario.startTime) / 1000;
  if (elapsed >= measuredDuration) return;
  const selection = sequence(slot);
  const userIndex = userIndexFor(selection, data.sessions.length);
  let request;
  try { request = operation(slot, userIndex, selection, data.sessions.length); }
  catch (error) { emit({ type: 'invalid_corpus', error: String(error) }); exec.test.abort(String(error)); return; }
  const phase = elapsed < warmup ? 'warmup' : 'measurement';
  const tags = { phase, route: request.route, category: request.category };
  const event = { slot, ...tags, started_at_ms: begin, scenario_started_at_ms: exec.scenario.startTime };
  emit({ type: 'start', ...event });
  started.add(1, tags);
  const params = { headers: { Cookie: `${cookieName}=${data.sessions[userIndex]}` }, timeout: `${timeout}s`, tags };
  if (request.body !== undefined) params.headers['Content-Type'] = 'application/json';
  const response = http.request(request.method, `${baseURL}${request.path}`, request.body, params);
  const end = Date.now();
  const success = isEffectiveSuccess(request, response.status);
  let code = 'none';
  let body;
  try { body = response.json(); code = body.code || (body.error || {}).code || 'none'; } catch (_) { /* 空响应没有业务错误码。 */ }
  if (!['none', 'backend_busy', 'unauthorized', 'not_found', 'rate_limited'].includes(code)) code = 'other';
  const clientError = response.status !== 0 ? 'none' : Number(response.error_code) === 1050 || String(response.error).includes('timeout') ? 'timeout' : 'transport';
  const sample = request.category.endsWith('_write') ? {
    actor: userIndex, path: request.path, expected: request.body ? JSON.parse(request.body) : null,
    response_id: body && body.data && body.data.id, command_id: response.headers['X-Command-Id'] || null,
  } : undefined;
  emit({ type: 'outcome', ...event, completed_at_ms: end, success, status: response.status, business_code: code,
    client_error: clientError, http_ms: response.timings.duration, end_to_end_ms: end - begin, sample });
  completed.add(1, tags); latency.add(response.timings.duration, tags); endToEnd.add(end - begin, tags);
  if (request.login && success && response.cookies[cookieName] && response.cookies[cookieName][0]) {
    data.sessions[userIndex] = response.cookies[cookieName][0].value;
  }
}

export function health() {
  const begin = Date.now();
  const elapsed = (begin - exec.scenario.startTime) / 1000;
  if (elapsed >= measuredDuration) return;
  emit({ type: 'clock', scenario_started_at_ms: exec.scenario.startTime, elapsed_seconds: (begin - exec.scenario.startTime) / 1000 });
  const response = http.get(`${baseURL}/ready`, { timeout: `${timeout}s`, tags: { phase: 'health', route: 'GET /ready' } });
  emit({ type: 'health', timestamp_ms: Date.now(), status: response.status });
}

export function handleSummary(data) {
  return { [__ENV.LOAD_SUMMARY_PATH]: JSON.stringify(data) };
}
