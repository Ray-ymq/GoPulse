import http from 'k6/http';
import { windowAt, elapsedWindows } from './windows.mjs';
import { createSequence, userIndexFor, ownedIndexFor, postIndexFor, isEffectiveSuccess } from './sequence.mjs';
import { check } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate, Trend } from 'k6/metrics';
import { SharedArray } from 'k6/data';

const baseURL = String(__ENV.GOPULSE_BASE_URL || '').replace(/\/$/, '');
const cookieName = __ENV.LOAD_COOKIE_NAME || 'gopulse_session';
const timeoutSeconds = Number(__ENV.LOAD_REQUEST_TIMEOUT_SECONDS || 5);
const targetRPS = Number(__ENV.LOAD_TARGET_RPS || 10);
const warmupSeconds = Number(__ENV.LOAD_WARMUP_SECONDS || 0);
const measurementSeconds = Number(__ENV.LOAD_MEASUREMENT_SECONDS || 1);
const stages = JSON.parse(__ENV.LOAD_STAGES_JSON || '[]');
const continuous = stages.length > 0;
const virtualUsers = Number(__ENV.LOAD_VUS || 1);
const mix = JSON.parse(__ENV.LOAD_MIX_JSON || '[]');
const profile = __ENV.LOAD_PROFILE || 'smooth-mixed';
const sequence = createSequence(mix, profile);
const hotPosts = (Number(__ENV.LOAD_HOT_POSTS) || 16);
// Every corpus column is a separate SharedArray. Sharing one composite object
// made k6 copy the whole ~1MB corpus into each VU, so the load generator became
// the memory bottleneck at the default 1024 virtual users. Primitive columns
// stay shared and read-only.
function sharedCorpusField(name) {
  return new SharedArray(name, function () {
    return JSON.parse(open(__ENV.LOAD_CORPUS_PATH))[name];
  });
}
const readPostIDs = sharedCorpusField('read_post_ids');
const interactionPostIDs = sharedCorpusField('interaction_post_ids');
const editablePostIDs = sharedCorpusField('editable_post_ids');
const deletePostIDs = sharedCorpusField('delete_post_ids');
const corpusUsers = sharedCorpusField('users');
const credentialUsers = new SharedArray('credential_users', function () {
  return JSON.parse(open(__ENV.LOAD_CREDENTIALS_PATH)).users;
});
const credentialPassword = new SharedArray('credential_password', function () {
  return [JSON.parse(open(__ENV.LOAD_CREDENTIALS_PATH)).password];
})[0];

const businessRequests = new Counter('business_requests');
const corpusExhausted = new Counter('corpus_exhausted');
const businessLatency = new Trend('business_latency', true);
const businessSuccessLatency = new Trend('business_success_latency', true);
const businessSuccessRate = new Rate('business_success_rate');
const earlySuccessRate = new Rate('early_success_rate');
const errors429 = new Counter('errors_429');
const errors503 = new Counter('errors_503');
const errorsTimeout = new Counter('errors_timeout');
const errorsTransport = new Counter('errors_transport');
const errorsUnexpected = new Counter('errors_unexpected');
const setupLoginFailures = new Counter('setup_login_failures');
const healthRequests = new Counter('health_requests');
const healthSuccessRate = new Rate('health_success_rate');

const phaseMetrics = {
  warmup: makePhaseMetrics('warmup'),
  measurement: makePhaseMetrics('measurement'),
};
const phaseErrorMetrics = {
  warmup: makeErrorMetrics('warmup'),
  measurement: makeErrorMetrics('measurement'),
};

const routeTemplates = [
  ['read', 'GET', 'GET /api/v1/posts'],
  ['read', 'GET', 'GET /api/v1/posts/:postId'],
  ['read', 'GET', 'GET /api/v1/posts/following'],
  ['read', 'GET', 'GET /api/v1/posts/:postId/comments'],
  ['read', 'GET', 'GET /api/v1/users/:username'],
  ['search', 'GET', 'GET /api/v1/search/posts'],
  ['search', 'GET', 'GET /api/v1/search/users'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/notifications'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/bookmarks'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/users/me/following'],
  ['notification_bookmark_read', 'GET', 'GET /api/v1/users/me/followers'],
  ['content_write', 'POST', 'POST /api/v1/posts'],
  ['content_write', 'POST', 'POST /api/v1/posts/:postId/comments'],
  ['content_write', 'PATCH', 'PATCH /api/v1/posts/:postId'],
  ['content_write', 'DELETE', 'DELETE /api/v1/posts/:postId'],
  ['interaction_write', 'PUT', 'PUT /api/v1/posts/:postId/like'],
  ['interaction_write', 'DELETE', 'DELETE /api/v1/posts/:postId/like'],
  ['interaction_write', 'PUT', 'PUT /api/v1/users/:userId/follow'],
  ['interaction_write', 'DELETE', 'DELETE /api/v1/users/:userId/follow'],
  ['interaction_write', 'PUT', 'PUT /api/v1/posts/:postId/bookmark'],
  ['identity_session', 'POST', 'POST /api/v1/auth/login'],
  ['identity_session', 'GET', 'GET /api/v1/users/me'],
];

const routeMetrics = {
  warmup: makeRouteMetrics('warmup'),
  measurement: makeRouteMetrics('measurement'),
};

const stageMetrics = stages.map((_, index) => Object.fromEntries(['warmup', 'measurement'].map((phase) => [phase, makePhaseMetrics(`stage${index}_${phase}`)])));
const stageErrors = stages.map((_, index) => Object.fromEntries(['warmup', 'measurement'].map((phase) => [phase, makeErrorMetrics(`stage${index}_${phase}`)])));
const stageRoutes = stages.map((_, index) => Object.fromEntries(['warmup', 'measurement'].map((phase) => [phase, makeRouteMetrics(`stage${index}_${phase}`)])));
const stageClock = new Trend('stage_clock_seconds');
const scenarioStarted = new Trend('scenario_started_at_ms');

let scenarios = {
  measurement: {
    executor: 'constant-arrival-rate',
    rate: Math.round(targetRPS * 100),
    timeUnit: '100s',
    duration: `${measurementSeconds}s`,
    preAllocatedVUs: virtualUsers,
    maxVUs: Math.max(virtualUsers * 2, virtualUsers + 100),
    exec: 'business',
    startTime: `${warmupSeconds}s`,
  },
  health: {
    executor: 'constant-arrival-rate',
    rate: 1,
    timeUnit: '1s',
    duration: `${warmupSeconds + measurementSeconds}s`,
    preAllocatedVUs: 1,
    maxVUs: 2,
    exec: 'health',
    startTime: '0s',
  },
};
if (warmupSeconds > 0) {
  scenarios.warmup = {
    executor: 'constant-arrival-rate',
    rate: Math.round(targetRPS * 100),
    timeUnit: '100s',
    duration: `${warmupSeconds}s`,
    preAllocatedVUs: virtualUsers,
    maxVUs: Math.max(virtualUsers * 2, virtualUsers + 100),
    exec: 'business',
    startTime: '0s',
  };
}

if (continuous) {
  const ramps = [];
  for (const stage of stages) {
    ramps.push({ duration: '0s', target: Math.round(stage.target_rps * 100) });
    ramps.push({ duration: `${stage.warmup_seconds + stage.measurement_seconds}s`, target: Math.round(stage.target_rps * 100) });
  }
  const duration = stages.reduce((sum, stage) => sum + stage.warmup_seconds + stage.measurement_seconds, 0);
  scenarios = {
    continuous: { executor: 'ramping-arrival-rate', startRate: Math.round(stages[0].target_rps * 100), timeUnit: '100s', stages: ramps,
      preAllocatedVUs: virtualUsers, maxVUs: virtualUsers, exec: 'business', gracefulStop: `${timeoutSeconds}s` },
    health: { executor: 'constant-arrival-rate', rate: 1, timeUnit: '1s', duration: `${duration}s`,
      preAllocatedVUs: 1, maxVUs: 1, exec: 'health', gracefulStop: `${timeoutSeconds}s` },
  };
}

export const options = {
  scenarios,
  systemTags: ['status', 'method', 'scenario', 'error_code', 'expected_response'],
  // Authenticating every deterministic user during setup is CPU-bound. Batch
  // the logins and keep a generous setup budget so a slow host cannot turn a
  // capacity stage into a setup timeout.
  setupTimeout: '300s',
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  thresholds: continuous ? {} : {
    business_success_rate: [{
      threshold: `rate>${1 - Number(__ENV.LOAD_MAX_ERROR_RATE || 0.01)}`,
      abortOnFail: !continuous,
      delayAbortEval: `${Number(__ENV.LOAD_EARLY_STOP_SECONDS || 30)}s`,
    }],
    early_success_rate: [`rate>${1 - Number(__ENV.LOAD_EARLY_STOP_ERROR_RATE || 0.2)}`],
    health_success_rate: [{
      threshold: 'rate>0.999',
      abortOnFail: !continuous,
      delayAbortEval: `${Number(__ENV.LOAD_HEALTH_FAILURE_SECONDS || 30)}s`,
    }],
  },
};

export function setup() {
  const setupStarted = Date.now();
  const sessions = [];
  // Authenticate one session per virtual user (bounded by the credential
  // file); VUs cycle through the resulting sessions so per-identity quotas
  // stay realistic with fewer VUs than credentials.
  const count = Math.min(virtualUsers, credentialUsers.length);
  const loginBatchSize = 16;
  for (let start = 0; start < count; start += loginBatchSize) {
    const end = Math.min(start + loginBatchSize, count);
    const requests = [];
    for (let index = start; index < end; index += 1) {
      const user = credentialUsers[index];
      requests.push({
        method: 'POST',
        url: `${baseURL}/api/v1/auth/login`,
        body: JSON.stringify({ username: user.username, password: credentialPassword }),
        params: {
          headers: { 'Content-Type': 'application/json' },
          timeout: `${timeoutSeconds}s`,
          tags: { phase: 'setup', operation: 'login', category: 'identity_session', route: 'POST /api/v1/auth/login', method: 'POST' },
        },
      });
    }
    const responses = http.batch(requests);
    if (responses.length !== requests.length) {
      throw new Error('setup login batch returned an unexpected response count');
    }
    for (let offset = 0; offset < responses.length; offset += 1) {
      const response = responses[offset];
      const passed = check(response, { 'setup login succeeded': (value) => value.status === 200 });
      const cookie = response.cookies[cookieName] && response.cookies[cookieName][0];
      if (!passed || !cookie || !cookie.value) {
        setupLoginFailures.add(1, { phase: 'setup' });
        throw new Error(`setup login failed for deterministic user ${start + offset + 1}`);
      }
      sessions.push(cookie.value);
    }
  }
  if (sessions.length === 0) {
    throw new Error('credentials contain no users');
  }
  return { sessions, setupElapsedMs: Date.now() - setupStarted };
}

export function business(data) {
  // A per-VU iteration counter would keep most VUs at slot 0 and turn the
  // reviewed mix into a feed-read benchmark. The global iteration index makes
  // the declared category and route split hold for every load level.
  const slot = exec.scenario.iterationInTest;
  if (slot === 0) scenarioStarted.add(exec.scenario.startTime - (!continuous && exec.scenario.name === 'measurement' ? warmupSeconds * 1000 : 0));
  const globalSlot = slot + (exec.scenario.name === 'measurement' ? Math.ceil(warmupSeconds * targetRPS) : 0);
  const selection = sequence(globalSlot);
  const userIndex = userIndexFor(selection, data.sessions.length);
  let request;
  try { request = operation(globalSlot, userIndex, selection, data.sessions.length); }
  catch (error) { corpusExhausted.add(1); exec.test.abort(String(error)); return; }
  const elapsed = (Date.now() - exec.scenario.startTime) / 1000;
  const window = continuous ? windowAt(elapsed, stages) : null;
  const phase = continuous ? window.phase : exec.scenario.name === 'warmup' ? 'warmup' : 'measurement';
  const tags = {
    phase,
    stage: continuous ? String(window.index) : '0',
    operation: 'business',
    category: request.category,
    route: request.route,
    method: request.method,
  };
  const params = {
    headers: { Cookie: `${cookieName}=${data.sessions[userIndex]}` },
    timeout: `${timeoutSeconds}s`,
    tags,
  };
  if (request.body !== undefined) {
    params.headers['Content-Type'] = 'application/json';
  }
  const response = http.request(request.method, `${baseURL}${request.path}`, request.body, params);
  const passed = check(response, { 'business response is expected': (value) => isEffectiveSuccess(request, value.status) }, tags);
  businessRequests.add(1, tags);
  if (passed) businessSuccessLatency.add(response.timings.duration, tags);
  businessLatency.add(response.timings.duration, tags);
  businessSuccessRate.add(passed, tags);
  earlySuccessRate.add(passed, tags);
  phaseMetrics[phase].requests.add(1);
  phaseMetrics[phase].latency.add(response.timings.duration);
  phaseMetrics[phase].success.add(passed);
  const routeMetric = routeMetrics[phase][routeIndex[request.route]];
  routeMetric.requests.add(1);
  routeMetric.latency.add(response.timings.duration);
  routeMetric.success.add(passed);
  if (continuous) {
    const metrics = stageMetrics[window.index][phase];
    metrics.requests.add(1);
    metrics.latency.add(response.timings.duration);
    metrics.success.add(passed);
    if (passed) metrics.successLatency.add(response.timings.duration);
    const route = stageRoutes[window.index][phase][routeIndex[request.route]];
    route.requests.add(1); route.latency.add(response.timings.duration); route.success.add(passed);
    if (request.route === 'DELETE /api/v1/posts/:postId') {
      if (response.status === 204) metrics.deleted.add(1);
      if (response.status === 404) metrics.alreadyAbsent.add(1);
    }
  }
  if (passed) phaseMetrics[phase].successLatency.add(response.timings.duration);
  classifyResponse(response, passed, tags);
  if (request.route === 'DELETE /api/v1/posts/:postId') {
    if (response.status === 204) phaseMetrics[phase].deleted.add(1);
    if (response.status === 404) phaseMetrics[phase].alreadyAbsent.add(1);
  }
  if (request.login && response.status === 200) {
    const cookie = response.cookies[cookieName] && response.cookies[cookieName][0];
    if (cookie && cookie.value) data.sessions[userIndex] = cookie.value;
  }
}

export function health() {
  scenarioStarted.add(exec.scenario.startTime);
  if (continuous) stageClock.add((Date.now() - exec.scenario.startTime) / 1000);
  const response = http.get(__ENV.LOAD_HEALTH_URL, {
    timeout: `${timeoutSeconds}s`,
    tags: { phase: 'health', operation: 'health', route: 'GET /ready', method: 'GET' },
  });
  healthRequests.add(1, { phase: 'health' });
  healthSuccessRate.add(response.status >= 200 && response.status < 300, { phase: 'health' });
}

function classifyResponse(response, passed, tags) {
  const phaseMetricsForErrors = phaseErrorMetrics[tags.phase];
  const staged = continuous ? stageErrors[Number(tags.stage)][tags.phase] : null;
  if (response.status === 429) {
    errors429.add(1, tags);
    phaseMetricsForErrors.errors429.add(1);
    if (staged) staged.errors429.add(1);
    return;
  }
  if (response.status === 503) {
    errors503.add(1, tags);
    phaseMetricsForErrors.errors503.add(1);
    if (staged) staged.errors503.add(1);
    return;
  }
  if (response.status === 0) {
    const error = String(response.error || '').toLowerCase();
    if (error.includes('timeout') || response.timings.duration >= timeoutSeconds * 1000) {
      errorsTimeout.add(1, tags);
      phaseMetricsForErrors.errorsTimeout.add(1);
    if (staged) staged.errorsTimeout.add(1);
    } else {
      errorsTransport.add(1, tags);
      phaseMetricsForErrors.errorsTransport.add(1);
    if (staged) staged.errorsTransport.add(1);
    }
    return;
  }
  if (!passed) {
    errorsUnexpected.add(1, tags);
    phaseMetricsForErrors.errorsUnexpected.add(1);
    if (staged) staged.errorsUnexpected.add(1);
  }
}

function operation(slot, userIndex, selection, users) {
  const category = selection.category;
  const postID = readPostIDs[postIndexFor(profile === 'smooth-mixed' ? selection.ordinal : slot, readPostIDs.length, profile, hotPosts)];
  const user = corpusUsers[indexFor(slot, corpusUsers.length)];
  if (category === 'read') {
    switch (selection.route) {
      case 0: return get(category, 'GET /api/v1/posts', '/api/v1/posts?limit=20', [200]);
      case 1: return get(category, 'GET /api/v1/posts/:postId', `/api/v1/posts/${postID}`, [200]);
      case 2: return get(category, 'GET /api/v1/posts/following', '/api/v1/posts/following?limit=20', [200]);
      case 3: return get(category, 'GET /api/v1/posts/:postId/comments', `/api/v1/posts/${postID}/comments?limit=20`, [200]);
      default: return get(category, 'GET /api/v1/users/:username', `/api/v1/users/${encodeURIComponent(user.username)}`, [200]);
    }
  }
  if (category === 'search') {
    const queries = ['phase18', `post ${String((slot % 50000) + 1).padStart(5, '0')}`, 'go'];
    const query = encodeURIComponent(queries[selection.ordinal % queries.length]);
    if (selection.route === 0) return get(category, 'GET /api/v1/search/posts', `/api/v1/search/posts?q=${query}&limit=20`, [200]);
    return get(category, 'GET /api/v1/search/users', `/api/v1/search/users?q=${query}&limit=20`, [200]);
  }
  if (category === 'notification_bookmark_read') {
    switch (selection.route) {
      case 0: return get(category, 'GET /api/v1/notifications', '/api/v1/notifications?limit=20', [200]);
      case 1: return get(category, 'GET /api/v1/bookmarks', '/api/v1/bookmarks?limit=20', [200]);
      case 2: return get(category, 'GET /api/v1/users/me/following', '/api/v1/users/me/following?limit=20', [200]);
      default: return get(category, 'GET /api/v1/users/me/followers', '/api/v1/users/me/followers?limit=20', [200]);
    }
  }
  if (category === 'content_write') {
    switch (selection.route) {
      case 0: return post(category, 'POST /api/v1/posts', '/api/v1/posts', { title: `k6 post ${__VU}-${slot}`, content: `deterministic content ${slot}` }, [201]);
      case 1: return post(category, 'POST /api/v1/posts/:postId/comments', `/api/v1/posts/${postID}/comments`, { content: `k6 comment ${slot}` }, [201]);
      case 2: {
        const editable = editablePostIDs[userIndex];
        const editableID = editable[ownedIndexFor(selection, users, editable.length)];
        return post(category, 'PATCH /api/v1/posts/:postId', `/api/v1/posts/${editableID}`, { title: `k6 edit ${slot}`, content: `deterministic edit ${slot}` }, [200], 'PATCH');
      }
      default: {
        const deletable = deletePostIDs[userIndex];
        const deletableID = deletable[ownedIndexFor(selection, users, deletable.length, continuous)];
        return post(category, 'DELETE /api/v1/posts/:postId', `/api/v1/posts/${deletableID}`, undefined, [204, 404], 'DELETE');
      }
    }
  }
  if (category === 'interaction_write') {
    const interactionPost = interactionPostIDs[indexFor(slot, interactionPostIDs.length)];
    const target = corpusUsers[(userIndex + Math.floor(slot % Math.max(1, corpusUsers.length - 1)) + 1) % corpusUsers.length].id;
    switch (selection.route) {
      case 0: return get(category, 'PUT /api/v1/posts/:postId/like', `/api/v1/posts/${interactionPost}/like`, [204], 'PUT');
      case 1: return get(category, 'DELETE /api/v1/posts/:postId/like', `/api/v1/posts/${interactionPost}/like`, [204], 'DELETE');
      case 2: return get(category, 'PUT /api/v1/users/:userId/follow', `/api/v1/users/${target}/follow`, [200], 'PUT');
      case 3: return get(category, 'DELETE /api/v1/users/:userId/follow', `/api/v1/users/${target}/follow`, [200], 'DELETE');
      default: return get(category, 'PUT /api/v1/posts/:postId/bookmark', `/api/v1/posts/${interactionPost}/bookmark`, [204], 'PUT');
    }
  }
  if (selection.route === 0) {
    const loginUser = credentialUsers[userIndex];
    return post('identity_session', 'POST /api/v1/auth/login', '/api/v1/auth/login', { username: loginUser.username, password: credentialPassword }, [200], 'POST', true);
  }
  return get('identity_session', 'GET /api/v1/users/me', '/api/v1/users/me', [200]);
}

const routeIndex = Object.fromEntries(routeTemplates.map((entry, index) => [entry[2], index]));

function makeRouteMetrics(phase) {
  return routeTemplates.map((_, index) => ({
    requests: new Counter(`route_requests_${phase}_${index}`),
    latency: new Trend(`route_latency_${phase}_${index}`, true),
    success: new Rate(`route_success_${phase}_${index}`),
  }));
}

function makePhaseMetrics(phase) {
  return {
    requests: new Counter(`business_requests_${phase}`),
    deleted: new Counter(`delete_succeeded_${phase}`),
    alreadyAbsent: new Counter(`delete_already_absent_${phase}`),
    latency: new Trend(`business_latency_${phase}`, true),
    successLatency: new Trend(`business_success_latency_${phase}`, true),
    success: new Rate(`business_success_rate_${phase}`),
  };
}

function makeErrorMetrics(phase) {
  return {
    errors429: new Counter(`errors_429_${phase}`),
    errors503: new Counter(`errors_503_${phase}`),
    errorsTimeout: new Counter(`errors_timeout_${phase}`),
    errorsTransport: new Counter(`errors_transport_${phase}`),
    errorsUnexpected: new Counter(`errors_unexpected_${phase}`),
  };
}

function indexFor(slot, size) {
  if (size <= 1) return 0;
  return Math.abs(slot ^ Math.floor(slot / 131071)) % size;
}

function get(category, route, path, statuses, method = 'GET') {
  return { category, route, path, statuses, method };
}

function post(category, route, path, body, statuses, method = 'POST', login = false) {
  return { category, route, path, statuses, method, login, body: body === undefined ? undefined : JSON.stringify(body) };
}

// k6 的场景计时不含 setup，窗口时长不会用目标请求数反推。
export function handleSummary(data) {
  const seconds = !data.metrics.scenario_started_at_ms ? 0 : Math.max(0, (data.state.testRunDurationMs - ((data.setup_data || {}).setupElapsedMs || 0)) / 1000);
  const windows = elapsedWindows(seconds, continuous ? stages : [{ warmup_seconds: warmupSeconds, measurement_seconds: measurementSeconds }]);
  const metrics = Object.fromEntries(Object.entries(data.metrics).map(([name, metric]) => [name, metric.values]));
  for (let index = 0; index < windows.length; index += 1) {
    for (const phase of ['warmup', 'measurement']) {
      const suffix = continuous ? `stage${index}_${phase}` : phase;
      metrics[`window_seconds_${suffix}`] = { value: windows[index][phase] };
    }
  }
  return { [__ENV.LOAD_SUMMARY_PATH || 'summary.json']: JSON.stringify({ metrics, timing: { test_run_seconds: seconds, windows } }) };
}
