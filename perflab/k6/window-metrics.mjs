class WindowMetric {
  constructor(metric, name) {
    this.metric = metric;
    this.name = name;
  }

  add(value) {
    this.metric.add(value, { summary_metric: this.name });
  }
}

// 各 VU 只创建三个原生指标对象；固定标签区分窗口和路由。
export function createWindowMetrics({ Counter, Rate, Trend }, collectSummary = true) {
  const names = { counter: 'window_summary_count', rate: 'window_summary_rate', trend: 'window_summary_latency' };
  const metrics = { counter: new Counter(names.counter), rate: new Rate(names.rate), trend: new Trend(names.trend, true) };
  const selectors = new Map();
  function create(kind, name) {
    if (collectSummary) selectors.set(name, `${names[kind]}{summary_metric:${name}}`);
    return new WindowMetric(metrics[kind], name);
  }
  return {
    counter: (name) => create('counter', name),
    rate: (name) => create('rate', name),
    trend: (name) => create('trend', name),
    // 空阈值只要求 k6 聚合子指标，不增加通过条件或停压条件。
    thresholds: () => Object.fromEntries(Array.from(selectors.values(), (selector) => [selector, []])),
    summary(data) {
      const result = Object.fromEntries(Object.entries(data.metrics)
        .filter(([name]) => !name.startsWith('window_summary_'))
        .map(([name, metric]) => [name, metric.values]));
      for (const [name, selector] of selectors) {
        if (data.metrics[selector]) result[name] = data.metrics[selector].values;
      }
      return result;
    },
  };
}
