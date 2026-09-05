import { afterEach, describe, expect, it, vi } from 'vitest'
import { isEventEntry, isMetricResult, observabilityApi } from './observability'

function response(body: unknown): Response { return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }) }
afterEach(() => vi.unstubAllGlobals())
describe('observability runtime boundary', () => {
  it('accepts a fixed metric DTO and safely encodes the catalog query', async () => {
    const payload = { metric:'gopulse_redis_up',kind:'gauge',unit:'boolean',range:'15m',from:'2026-09-05T08:00:00Z',to:'2026-09-05T08:15:00Z',step_seconds:15,series:[{labels:{},points:[{timestamp:'2026-09-05T08:15:00Z',value:1}]}] }
    const fetchMock = vi.fn().mockResolvedValue(response({ data: payload })); vi.stubGlobal('fetch', fetchMock)
    await expect(observabilityApi.metrics('gopulse_redis_up','15m')).resolves.toEqual(payload)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/observability/metrics?metric=gopulse_redis_up&range=15m')
  })
  it('rejects unknown metric and event fields', () => {
    expect(isMetricResult({ metric:'gopulse_redis_up',kind:'gauge',unit:'boolean',range:'15m',from:'2026-09-05T08:00:00Z',to:'2026-09-05T08:15:00Z',step_seconds:15,series:[],query:'secret' })).toBe(false)
    expect(isEventEntry({ timestamp:'2026-09-05T08:00:00Z',event_name:'exporter_plugin_started',source:'monitor',severity:'info',message:'exporter plugin started',metadata:{plugin_id:'redis-exporter'},index:'private' })).toBe(false)
  })
  it('rejects impossible metric and event contracts', () => {
    expect(isMetricResult({ metric:'gopulse_redis_up',kind:'gauge',unit:'boolean',range:'15m',from:'2026-09-05T08:15:00Z',to:'2026-09-05T08:00:00Z',step_seconds:999,series:[{labels:{},points:[{timestamp:'2026-09-05T08:10:00Z',value:1},{timestamp:'2026-09-05T08:09:00Z',value:1}]},{labels:{},points:[]}] })).toBe(false)
    expect(isEventEntry({ timestamp:'2026-09-05T08:00:00Z',event_name:'exporter_plugin_started',source:'monitor',severity:'info',message:'arbitrary',metadata:{plugin_id:'redis-exporter',operation:'start'} })).toBe(false)
    expect(isEventEntry({ timestamp:'2026-09-05T08:00:00Z',event_name:'exporter_plugin_started',source:'monitor',severity:'info',message:'exporter plugin started',metadata:{plugin_id:'redis-exporter',plugin_version:'1.8.4',operation:'start',from_state:'stopped',to_state:'running'} })).toBe(true)
  })
  it('sends opaque cursor without replaying filters', async () => {
    const fetchMock=vi.fn().mockResolvedValue(response({data:[],meta:{next_cursor:null}})); vi.stubGlobal('fetch',fetchMock)
    await observabilityApi.logs({range:'15m',service:'backend',module:'http',level:'',message:'',request_id:'',event_id:'',error_code:''},'opaque+/=')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/observability/logs?cursor=opaque%2B%2F%3D')
  })
})
