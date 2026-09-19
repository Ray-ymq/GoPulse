import { expect, test } from '@playwright/test'

// Candidate-only acceptance: six actual target collectors through the existing
// Monitor -> Router -> Marshaller -> Backend path, not synthetic metric writes.
test('candidate six-plugin collection and scoped failure', async ({ request }) => {
  test.setTimeout(240_000)
  const token = process.env.GOPULSE_ACCEPTANCE_TOKEN ?? ''
  const api = '/api/v1/'
  const login = await request.post(api + 'auth/login', { data: {
    username: process.env.GOPULSE_OBSERVABILITY_ADMIN_USERNAME,
    password: process.env.GOPULSE_OBSERVABILITY_PASSWORD,
  } })
  expect(login.ok()).toBeTruthy()
  const sources = ['redis', 'mysql', 'rabbitmq', 'kafka', 'elasticsearch', 'victoriametrics']
  const common = { connect_timeout: '1s', scrape_timeout: '3s' }
  const configurations: Record<string, { config: Record<string, unknown>; secrets: Record<string, string> }> = {
    mysql: { config: { ...common, host: 'mysql', port: 3306, database: `gopulse_${token}`, username: 'gopulse_metrics' }, secrets: { password: `metrics-${token}` } },
    rabbitmq: { config: { ...common, host: 'rabbitmq', management_port: 15672, vhost: '/', username: 'gopulse_metrics' }, secrets: { password: `metrics-${token}` } },
    kafka: { config: { ...common, host: 'kafka', port: 19092, topic: 'gopulse-observability-v1', consumer_group: 'gopulse-marshaller-metrics-v1' }, secrets: {} },
    elasticsearch: { config: { ...common, host: 'elasticsearch', port: 9200 }, secrets: {} },
    victoriametrics: { config: { ...common, host: 'victoriametrics', port: 8428, username: `vm_${token}` }, secrets: { password: `vm-${token}-0123456789abcdef0123456789abc` } },
  }
  for (const source of sources.filter(s => s !== 'redis')) {
    const response = await request.post(api + `exporter-plugins/${source}-exporter/install`, { data: configurations[source] })
    expect(response.status(), `${source} install`).toBe(201)
  }
  async function status(source: string) {
    const response = await request.get(api + `exporter-plugins/${source}-exporter`)
    expect(response.ok()).toBeTruthy()
    return (await response.json()).data
  }
  async function collected(source: string) {
    const response = await request.get(api + `observability/metrics?metric=gopulse_${source}_up&range=15m`)
    if (!response.ok()) return false
    const data = (await response.json()).data
    return data.series.some((series: { points: { value: number }[] }) => series.points.some(point => point.value === 1))
  }
  for (const source of sources) {
    await expect.poll(async () => (await status(source)).last_success_at, { timeout: 60_000 }).toBeTruthy()
    await expect.poll(() => collected(source), { timeout: 60_000 }).toBe(true)
  }
  // Stop exactly one collector; prove sibling collections advance and the
  // application remains reachable. Shared dependencies are left untouched.
  const before = (await status('redis')).last_success_at
  const stop = await request.post(api + 'exporter-plugins/mysql-exporter/stop')
  expect(stop.ok()).toBeTruthy()
  await expect.poll(async () => (await status('mysql')).observed_state).toBe('stopped')
  await expect.poll(async () => (await status('redis')).last_success_at, { timeout: 45_000 }).not.toBe(before)
  expect((await request.get(api + 'posts')).ok()).toBeTruthy()
  expect((await request.post(api + 'exporter-plugins/mysql-exporter/start')).ok()).toBeTruthy()
  await expect.poll(async () => (await status('mysql')).observed_state).toBe('running')
})
