import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate, Trend } from 'k6/metrics';

const baseURL = String(__ENV.GOPULSE_BASE_URL || '').replace(/\/$/, '');
const cookieName = __ENV.LOAD_COOKIE_NAME || 'gopulse_session';
const timeoutSeconds = Number(__ENV.LOAD_REQUEST_TIMEOUT_SECONDS || 5);
const targetRPS = Number(__ENV.LOAD_TARGET_RPS || 10);
const warmupSeconds = Number(__ENV.LOAD_WARMUP_SECONDS || 0);
const measurementSeconds = Number(__ENV.LOAD_MEASUREMENT_SECONDS || 1);
const virtualUsers = Number(__ENV.LOAD_VUS || 1);
const mix = JSON.parse(__ENV.LOAD_MIX_JSON || '[]');
const corpus = JSON.parse(open(__ENV.LOAD_CORPUS_PATH));
const credentials = JSON.parse(open(__ENV.LOAD_CREDENTIALS_PATH));

const businessRequests = new Counter('business_requests');
const businessLatency = new Trend('business_latency', true);
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

const scenarios = {
  measurement: {
    executor: 'constant-arrival-rate',
    rate: targetRPS,
    timeUnit: '1s',
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
    rate: targetRPS,
    timeUnit: '1s',
    duration: `${warmupSeconds}s`,
    preAllocatedVUs: virtualUsers,
    maxVUs: Math.max(virtualUsers * 2, virtualUsers + 100),
    exec: 'business',
    startTime: '0s',
  };
}

export const options = {
  scenarios,
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(90)', 'p(95)', 'p(99)'],
  thresholds: {
    business_success_rate: [{
      threshold: `rate>${1 - Number(__ENV.LOAD_MAX_ERROR_RATE || 0.01)}`,
      abortOnFail: true,
      delayAbortEval: `${Number(__ENV.LOAD_EARLY_STOP_SECONDS || 30)}s`,
    }],
    early_success_rate: [`rate>${1 - Number(__ENV.LOAD_EARLY_STOP_ERROR_RATE || 0.2)}`],
    health_success_rate: [{
      threshold: 'rate>0.999',
      abortOnFail: true,
      delayAbortEval: `${Number(__ENV.LOAD_HEALTH_FAILURE_SECONDS || 30)}s`,
    }],
  },
};

export function setup() {
  const sessions = [];
  const count = Math.min(virtualUsers, credentials.users.length);
  for (let index = 0; index < count; index += 1) {
    const user = credentials.users[index];
    const response = http.post(`${baseURL}/api/v1/auth/login`, JSON.stringify({
      username: user.username,
      password: credentials.password,
    }), {
      headers: { 'Content-Type': 'application/json' },
      timeout: `${timeoutSeconds}s`,
      tags: { phase: 'setup', operation: 'login', category: 'identity_session', route: 'POST /api/v1/auth/login', method: 'POST' },
    });
    const passed = check(response, { 'setup login succeeded': (value) => value.status === 200 });
    const cookie = response.cookies[cookieName] && response.cookies[cookieName][0];
    if (!passed || !cookie || !cookie.value) {
      setupLoginFailures.add(1, { phase: 'setup' });
      throw new Error(`setup login failed for deterministic user ${index + 1}`);
    }
    sessions.push(cookie.value);
  }
  if (sessions.length < virtualUsers) {
    throw new Error(`credentials contain ${sessions.length} users, need ${virtualUsers}`);
  }
  return { corpus, credentials, sessions };
}

export function business(data) {
  const slot = __ITER;
  const userIndex = (__VU - 1) % data.sessions.length;
  const request = operation(slot, userIndex, data.corpus, data.credentials);
  const phase = exec.scenario.name === 'warmup' ? 'warmup' : 'measurement';
  const tags = {
    phase,
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
  const passed = check(response, { 'business response is expected': (value) => request.statuses.includes(value.status) }, tags);
  businessRequests.add(1, tags);
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
  classifyResponse(response, passed, tags);
  if (request.login && response.status === 200) {
    const cookie = response.cookies[cookieName] && response.cookies[cookieName][0];
    if (cookie && cookie.value) data.sessions[userIndex] = cookie.value;
  }
}

export function health() {
  const response = http.get(__ENV.LOAD_HEALTH_URL, {
    timeout: `${timeoutSeconds}s`,
    tags: { phase: 'health', operation: 'health', route: 'GET /ready', method: 'GET' },
  });
  healthRequests.add(1, { phase: 'health' });
  healthSuccessRate.add(response.status >= 200 && response.status < 300, { phase: 'health' });
}

function classifyResponse(response, passed, tags) {
  const phaseMetricsForErrors = phaseErrorMetrics[tags.phase];
  if (response.status === 429) {
    errors429.add(1, tags);
    phaseMetricsForErrors.errors429.add(1);
    return;
  }
  if (response.status === 503) {
    errors503.add(1, tags);
    phaseMetricsForErrors.errors503.add(1);
    return;
  }
  if (response.status === 0) {
    const error = String(response.error || '').toLowerCase();
    if (error.includes('timeout') || response.timings.duration >= timeoutSeconds * 1000) {
      errorsTimeout.add(1, tags);
      phaseMetricsForErrors.errorsTimeout.add(1);
    } else {
      errorsTransport.add(1, tags);
      phaseMetricsForErrors.errorsTransport.add(1);
    }
    return;
  }
  if (!passed) {
    errorsUnexpected.add(1, tags);
    phaseMetricsForErrors.errorsUnexpected.add(1);
  }
}

function operation(slot, userIndex, corpus, credentials) {
  const category = categoryForSlot(slot);
  const postID = corpus.read_post_ids[indexFor(slot, corpus.read_post_ids.length)];
  const user = corpus.users[indexFor(slot, corpus.users.length)];
  if (category === 'read') {
    switch (Math.floor(slot / 100) % 5) {
      case 0: return get(category, 'GET /api/v1/posts', '/api/v1/posts?limit=20', [200]);
      case 1: return get(category, 'GET /api/v1/posts/:postId', `/api/v1/posts/${postID}`, [200]);
      case 2: return get(category, 'GET /api/v1/posts/following', '/api/v1/posts/following?limit=20', [200]);
      case 3: return get(category, 'GET /api/v1/posts/:postId/comments', `/api/v1/posts/${postID}/comments?limit=20`, [200]);
      default: return get(category, 'GET /api/v1/users/:username', `/api/v1/users/${encodeURIComponent(user.username)}`, [200]);
    }
  }
  if (category === 'search') {
    const queries = ['phase18', `post ${String((slot % 50000) + 1).padStart(5, '0')}`, 'go'];
    const query = encodeURIComponent(queries[Math.floor(slot / 100) % queries.length]);
    if (Math.floor(slot / 100) % 2 === 0) return get(category, 'GET /api/v1/search/posts', `/api/v1/search/posts?q=${query}&limit=20`, [200]);
    return get(category, 'GET /api/v1/search/users', `/api/v1/search/users?q=${query}&limit=20`, [200]);
  }
  if (category === 'notification_bookmark_read') {
    switch (Math.floor(slot / 100) % 4) {
      case 0: return get(category, 'GET /api/v1/notifications', '/api/v1/notifications?limit=20', [200]);
      case 1: return get(category, 'GET /api/v1/bookmarks', '/api/v1/bookmarks?limit=20', [200]);
      case 2: return get(category, 'GET /api/v1/users/me/following', '/api/v1/users/me/following?limit=20', [200]);
      default: return get(category, 'GET /api/v1/users/me/followers', '/api/v1/users/me/followers?limit=20', [200]);
    }
  }
  if (category === 'content_write') {
    switch (Math.floor(slot / 100) % 4) {
      case 0: return post(category, 'POST /api/v1/posts', '/api/v1/posts', { title: `k6 post ${__VU}-${slot}`, content: `deterministic content ${slot}` }, [201]);
      case 1: return post(category, 'POST /api/v1/posts/:postId/comments', `/api/v1/posts/${postID}/comments`, { content: `k6 comment ${slot}` }, [201]);
      case 2: return post(category, 'PATCH /api/v1/posts/:postId', `/api/v1/posts/${postID}`, { title: `k6 edit ${slot}`, content: `deterministic edit ${slot}` }, [200], 'PATCH');
      default: return post(category, 'DELETE /api/v1/posts/:postId', `/api/v1/posts/${postID}`, undefined, [204], 'DELETE');
    }
  }
  if (category === 'interaction_write') {
    const interactionPost = corpus.interaction_post_ids[indexFor(slot, corpus.interaction_post_ids.length)];
    const target = corpus.users[(userIndex + Math.floor(slot % Math.max(1, corpus.users.length - 1)) + 1) % corpus.users.length].id;
    switch (Math.floor(slot / 100) % 5) {
      case 0: return get(category, 'PUT /api/v1/posts/:postId/like', `/api/v1/posts/${interactionPost}/like`, [204], 'PUT');
      case 1: return get(category, 'DELETE /api/v1/posts/:postId/like', `/api/v1/posts/${interactionPost}/like`, [204], 'DELETE');
      case 2: return get(category, 'PUT /api/v1/users/:userId/follow', `/api/v1/users/${target}/follow`, [204], 'PUT');
      case 3: return get(category, 'DELETE /api/v1/users/:userId/follow', `/api/v1/users/${target}/follow`, [204], 'DELETE');
      default: return get(category, 'PUT /api/v1/posts/:postId/bookmark', `/api/v1/posts/${interactionPost}/bookmark`, [204], 'PUT');
    }
  }
  if (Math.floor(slot / 100) % 5 === 0) {
    const loginUser = credentials.users[userIndex];
    return post('identity_session', 'POST /api/v1/auth/login', '/api/v1/auth/login', { username: loginUser.username, password: credentials.password }, [200], 'POST', true);
  }
  return get('identity_session', 'GET /api/v1/users/me', '/api/v1/users/me', [200]);
}

function categoryForSlot(slot) {
  let cumulative = 0;
  const selector = slot % 100;
  for (const entry of mix) {
    cumulative += entry.percent;
    if (selector < cumulative) return entry.category;
  }
  return mix[mix.length - 1].category;
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
    latency: new Trend(`business_latency_${phase}`, true),
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
