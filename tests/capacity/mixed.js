import http from 'k6/http';
import { Counter, Rate, Trend } from 'k6/metrics';

function readInteger(name, minimum, fallback) {
  const value = Number(__ENV[name] ?? fallback);
  if (!Number.isSafeInteger(value) || value < minimum) {
    throw new Error(`${name} 必须是大于或等于 ${minimum} 的整数`);
  }
  return value;
}

const rate = readInteger('ARRIVAL_RATE', 1);
const maxVUs = readInteger('MAX_VUS', 500, 500);
const preAllocatedVUs = readInteger('PRE_ALLOCATED_VUS', 1, 128);
if (preAllocatedVUs > maxVUs) {
  throw new Error('PRE_ALLOCATED_VUS 不得超过 MAX_VUS');
}

if (!__ENV.SEED_FILE) {
  throw new Error('请设置 SEED_FILE');
}
const seed = JSON.parse(open(__ENV.SEED_FILE));
const baseURL = (__ENV.BASE_URL || seed.base_url || '').replace(/\/+$/, '');
const cookieName = __ENV.COOKIE_NAME || 'gopulse_session';
if (!baseURL || !seed.search_term) {
  throw new Error('请提供测试地址和搜索词');
}
if (!Array.isArray(seed.sessions) || seed.sessions.length === 0 ||
    !seed.sessions.every((entry) => typeof entry?.session === 'string' && entry.session.length > 0)) {
  throw new Error('SEED_FILE 必须包含至少一个会话值');
}
if (!Array.isArray(seed.posts) || seed.posts.length === 0) {
  throw new Error('SEED_FILE 必须包含至少一个帖子 ID');
}

export const options = {
  scenarios: {
    mixed: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration: __ENV.DURATION || '30m',
      preAllocatedVUs,
      maxVUs,
    },
  },
  discardResponseBodies: true,
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

const endpoints = ['timeline', 'detail', 'post_search', 'user_search'];
const endpointMetrics = Object.fromEntries(endpoints.map((name) => [name, {
  requests: new Counter(`${name}_requests`),
  errors: new Rate(`${name}_error_rate`),
  statuses503: new Rate(`${name}_503_rate`),
  latency: new Trend(`${name}_latency`, true),
}]));
const status503 = new Rate('mixed_503_rate');
const searchTerm = encodeURIComponent(seed.search_term);

export default function () {
  const requestIndex = __ITER + __VU;
  const selector = (requestIndex * 17) % 100;
  const { session } = seed.sessions[(__VU - 1) % seed.sessions.length];
  let endpoint;
  let path;

  if (selector < 40) {
    endpoint = 'timeline';
    path = '/api/v1/posts?limit=20';
  } else if (selector < 60) {
    endpoint = 'detail';
    const postID = seed.posts[requestIndex % seed.posts.length];
    path = `/api/v1/posts/${postID}`;
  } else if (selector < 85) {
    endpoint = 'post_search';
    path = `/api/v1/search/posts?q=${searchTerm}`;
  } else {
    endpoint = 'user_search';
    path = `/api/v1/search/users?q=${searchTerm}`;
  }

  const response = http.get(`${baseURL}${path}`, {
    headers: { Cookie: `${cookieName}=${session}`, Accept: 'application/json' },
    tags: { endpoint },
  });
  const metrics = endpointMetrics[endpoint];
  metrics.requests.add(1);
  metrics.errors.add(response.status < 200 || response.status >= 400);
  metrics.statuses503.add(response.status === 503);
  metrics.latency.add(response.timings.duration);
  status503.add(response.status === 503);
}
