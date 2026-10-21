import http from 'k6/http';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import { Counter, Trend } from 'k6/metrics';
import { createOperation } from '../k6/requests.mjs';
import { createSequence, userIndexFor, isEffectiveSuccess } from '../k6/sequence.mjs';

const vus = Number(__ENV.LOAD_VUS || 512);
const rps = Number(__ENV.LOAD_TARGET_RPS || 250);
const warmup = Number(__ENV.LOAD_WARMUP_SECONDS || 30);
const measurement = Number(__ENV.LOAD_MEASUREMENT_SECONDS || 300);
const timeout = Number(__ENV.LOAD_REQUEST_TIMEOUT_SECONDS || 5);
const offset = Number(__ENV.LOAD_SLOT_OFFSET || 0);
const base = __ENV.GOPULSE_BASE_URL;
function field(name) {
  return new SharedArray(`pt6_${name}`, () => JSON.parse(open(__ENV.LOAD_CORPUS_PATH))[name]);
}
const corpusUsers = field('users');
const credentialUsers = new SharedArray('pt6_credentials', () => JSON.parse(open(__ENV.LOAD_CREDENTIALS_PATH)).users);
const password = new SharedArray('pt6_password', () => [JSON.parse(open(__ENV.LOAD_CREDENTIALS_PATH)).password])[0];
const sequence = createSequence(JSON.parse(__ENV.LOAD_MIX_JSON));
const operation = createOperation({
  readPostIDs: field('read_post_ids'), interactionPostIDs: field('interaction_post_ids'),
  editablePostIDs: field('editable_post_ids'), deletePostIDs: field('delete_post_ids'),
  corpusUsers, credentialUsers, credentialPassword: password, profile: 'smooth-mixed',
  strictDelete: true, vu: () => __VU,
});
const started = new Counter('pt6_started');
const completed = new Counter('pt6_completed');
const successes = new Counter('pt6_successes');
const failures = new Counter('pt6_failures');
const latency = new Trend('pt6_latency', true);

export const options = {
  scenarios: { business: {
    executor: 'constant-arrival-rate', rate: rps, timeUnit: '1s', duration: `${warmup + measurement}s`,
    preAllocatedVUs: vus, maxVUs: vus, gracefulStop: `${timeout}s`,
  } },
  setupTimeout: '300s', discardResponseBodies: true,
  systemTags: ['status', 'method', 'name', 'scenario', 'error_code', 'expected_response'],
  summaryTrendStats: ['avg', 'p(95)', 'p(99)'],
  thresholds: {
    'pt6_started{phase:measurement}': [], 'pt6_completed{phase:measurement}': [],
    'pt6_successes{phase:measurement}': [],
    'pt6_latency{phase:measurement}': [`p(95)<=${__ENV.LOAD_MAX_P95_MS || 1000}`, `p(99)<=${__ENV.LOAD_MAX_P99_MS || 2000}`],
    pt6_failures: [{ threshold: 'count==0', abortOnFail: true }],
    dropped_iterations: [{ threshold: 'count==0', abortOnFail: true }],
  },
};

function stop(event) {
  console.error(`PT6 ${JSON.stringify({ type: 'first_failure', timestamp: new Date().toISOString(), ...event })}`);
  exec.test.abort('PT6 首次失败，立即停压');
}

export function setup() {
  const sessions = [];
  for (let index = 0; index < vus; index += 1) {
    const response = http.post(`${base}/api/v1/auth/login`, JSON.stringify({
      username: credentialUsers[index].username, password,
    }), { timeout: `${timeout}s`, headers: { 'Content-Type': 'application/json' },
      tags: { phase: 'setup', name: 'POST /api/v1/auth/login' } });
    const cookie = response.cookies.gopulse_session && response.cookies.gopulse_session[0];
    if (response.status !== 200 || !cookie || !cookie.value) {
      stop({ phase: 'setup', route: 'POST /api/v1/auth/login', status: response.status, error_code: response.error_code });
      return { sessions: [] };
    }
    sessions.push(cookie.value);
  }
  return { sessions };
}

export default function (data) {
  if (exec.scenario.iterationInTest === 0) {
    console.log(`PT6 ${JSON.stringify({ type: 'clock', scenario_started_at_ms: exec.scenario.startTime })}`);
  }
  const slot = offset + exec.scenario.iterationInTest;
  const phase = (Date.now() - exec.scenario.startTime) / 1000 < warmup ? 'warmup' : 'measurement';
  const selection = sequence(slot);
  const actor = userIndexFor(selection, data.sessions.length);
  let request;
  try { request = operation(slot, actor, selection, data.sessions.length); }
  catch (_) { stop({ phase, reason: 'corpus_exhausted', slot }); return; }
  const tags = { phase, name: request.route, category: request.category };
  const headers = { Cookie: `gopulse_session=${data.sessions[actor]}` };
  if (request.body !== undefined) headers['Content-Type'] = 'application/json';
  started.add(1, tags);
  const response = http.request(request.method, base + request.path, request.body,
    { headers, timeout: `${timeout}s`, tags });
  completed.add(1, tags);
  latency.add(response.timings.duration, tags);
  if (!isEffectiveSuccess(request, response.status)) {
    failures.add(1, tags);
    stop({ phase, route: request.route, slot, status: response.status, error_code: response.error_code });
    return;
  }
  successes.add(1, tags);
  failures.add(0, tags);
  if (request.login && response.cookies.gopulse_session && response.cookies.gopulse_session[0]) {
    data.sessions[actor] = response.cookies.gopulse_session[0].value;
  }
}

export function handleSummary(data) {
  delete data.setup_data;
  return { [__ENV.LOAD_SUMMARY_PATH]: JSON.stringify(data) };
}
