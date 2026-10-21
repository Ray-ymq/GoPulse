import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { createWindowMetrics } from './window-metrics.mjs';

function fixture() {
  const instances = [];
  class Metric {
    constructor(name, isTime) { this.name = name; this.isTime = isTime; this.samples = []; instances.push(this); }
    add(value, tags) { this.samples.push({ value, tags }); }
  }
  return { instances, constructors: { Counter: Metric, Rate: Metric, Trend: Metric } };
}

test('多个窗口复用三个原生指标，标签区分计数、成功率和延迟', () => {
  const { instances, constructors } = fixture();
  const metrics = createWindowMetrics(constructors);
  for (let stage = 0; stage < 9; stage += 1) {
    const name = `stage${stage}_measurement`;
    metrics.counter(`business_requests_${name}`).add(1);
    metrics.rate(`business_success_rate_${name}`).add(stage % 2 === 0);
    metrics.trend(`business_latency_${name}`).add(stage + 1);
  }
  assert.equal(instances.length, 3);
  assert.deepEqual(instances.map((metric) => metric.samples.length), [9, 9, 9]);
  assert.equal(instances[2].isTime, true);
  assert.deepEqual(instances[0].samples[8], { value: 1, tags: { summary_metric: 'business_requests_stage8_measurement' } });
  assert.equal(Object.keys(metrics.thresholds()).length, 27);
  assert(Object.values(metrics.thresholds()).every((thresholds) => thresholds.length === 0));
});

test('汇总恢复原字段，保留普通指标并移除内部聚合字段', () => {
  const { constructors } = fixture();
  const metrics = createWindowMetrics(constructors);
  metrics.counter('business_requests_measurement');
  metrics.trend('route_latency_stage0_measurement_1');
  metrics.rate('business_success_rate_measurement');
  const result = metrics.summary({ metrics: {
    business_requests: { values: { count: 8 } },
    window_summary_count: { values: { count: 12 } },
    'window_summary_count{summary_metric:business_requests_measurement}': { values: { count: 4, rate: 2 } },
    'window_summary_latency{summary_metric:route_latency_stage0_measurement_1}': { values: { 'p(95)': 38.5 } },
    'window_summary_rate{summary_metric:business_success_rate_measurement}': { values: { rate: .5, passes: 2, fails: 2 } },
  } });
  assert.deepEqual(result, {
    business_requests: { count: 8 }, business_requests_measurement: { count: 4, rate: 2 },
    route_latency_stage0_measurement_1: { 'p(95)': 38.5 },
    business_success_rate_measurement: { rate: .5, passes: 2, fails: 2 },
  });
});

test('业务 VU 可以创建窗口句柄，无须复制汇总配置或创建更多原生指标', () => {
  const { instances, constructors } = fixture();
  const metrics = createWindowMetrics(constructors, false);
  metrics.counter('business_requests_stage8_measurement').add(1);
  assert.deepEqual(metrics.thresholds(), {});
  assert.equal(instances.length, 3);
  assert.deepEqual(instances[0].samples[0].tags, { summary_metric: 'business_requests_stage8_measurement' });
});

const directory = path.dirname(fileURLToPath(import.meta.url));
const binary = process.env.K6_BIN || path.resolve(directory, '../../.run/tools/k6/0.56.0/linux-amd64/k6');
test('真实 k6 保留跨窗口计数、成功率和延迟分位数，且空阈值不会拒绝失败样本', { skip: !fs.existsSync(binary) }, () => {
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'gopulse-window-metrics-'));
  try {
    const output = path.join(temporary, 'summary.json');
    const script = path.join(temporary, 'test.js');
    fs.writeFileSync(script, `
import { Counter, Rate, Trend } from 'k6/metrics';
import exec from 'k6/execution';
import { createWindowMetrics } from ${JSON.stringify(path.join(directory, 'window-metrics.mjs'))};
const metrics = createWindowMetrics({ Counter, Rate, Trend }, __VU === 0);
function window(index) {
  return { count: metrics.counter('requests_stage' + index), success: metrics.rate('success_stage' + index), latency: metrics.trend('latency_stage' + index) };
}
if (__VU === 0) { window(0); window(1); }
export const options = { vus: 2, iterations: 8, thresholds: metrics.thresholds(), summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(95)', 'p(99)'] };
export default function () {
  const slot = exec.scenario.iterationInTest;
  const current = window(Math.floor(slot / 4));
  current.count.add(1); current.success.add(slot % 2 === 0); current.latency.add((slot % 4 + 1) * 10);
}
export function handleSummary(data) { return { [${JSON.stringify(output)}]: JSON.stringify(metrics.summary(data)) }; }
`);
    const result = spawnSync(binary, ['run', '--address', '', '--quiet', script], { encoding: 'utf8', timeout: 15000 });
    assert.equal(result.status, 0, result.stderr);
    const summary = JSON.parse(fs.readFileSync(output, 'utf8'));
    for (let stage = 0; stage < 2; stage += 1) {
      assert.equal(summary[`requests_stage${stage}`].count, 4);
      assert.deepEqual(summary[`success_stage${stage}`], { rate: .5, passes: 2, fails: 2 });
      assert.equal(summary[`latency_stage${stage}`].avg, 25);
      assert.equal(summary[`latency_stage${stage}`].med, 25);
      assert.equal(summary[`latency_stage${stage}`]['p(95)'], 38.5);
      assert(Math.abs(summary[`latency_stage${stage}`]['p(99)'] - 39.7) < 1e-9);
    }
    assert(!Object.keys(summary).some((name) => name.startsWith('window_summary_')));
  } finally {
    fs.rmSync(temporary, { recursive: true, force: true });
  }
});
