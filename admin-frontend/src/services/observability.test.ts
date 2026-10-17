import { afterEach, describe, expect, it, vi } from 'vitest'
import { componentContract } from './componentMetrics'
import { isEventEntry, isLogEntry, isMetricResult, observabilityApi } from './observability'
import { isMetricCatalog } from './observability'

function response(body: unknown): Response { return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }) }
afterEach(() => vi.unstubAllGlobals())

it('accepts an unregistered metric catalog and preserves queue and node dimensions', () => {
 const descriptor={metric:'gopulse_rabbitmq_queue_depth',kind:'gauge',unit:'none',source:'rabbitmq',target_id:'rabbitmq-exporter-local',producer_kind:'exporter_plugin',producer_id:'rabbitmq-exporter',label_keys:['node','queue']}
 expect(isMetricCatalog([descriptor])).toBe(true)
 expect(isMetricCatalog([])).toBe(true)
 expect(isMetricCatalog([descriptor,descriptor])).toBe(false)
 expect(isMetricCatalog([{...descriptor,label_keys:['producer_id']}])).toBe(false)
 const result={metric:descriptor.metric,kind:'gauge',unit:'none',range:'15m',from:'2026-10-14T08:00:00Z',to:'2026-10-14T08:15:00Z',step_seconds:15,series:[{labels:{queue:'jobs',node:'node-2'},points:[{timestamp:'2026-10-14T08:15:00Z',value:7}]}]}
 expect(isMetricResult(result)).toBe(true)
 expect(isMetricResult({...result,series:[{...result.series[0],labels:{source:'redis'}}]})).toBe(false)
 expect(isMetricResult({...result,metric:'sum(gopulse_rabbitmq_queue_depth)'})).toBe(false)
})
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
  it('accepts instance identity in log records while rejecting unknown fields', () => {
    const entry = { timestamp:'2026-09-05T08:00:00Z',level:'info',service:'backend',instance_id:'backend-1',module:'http',message:'http request completed',trace_id:'0102030405060708090a0b0c0d0e0f10',span_id:'0102030405060708',post_id:9,content_revision:1 }
    expect(isLogEntry(entry)).toBe(true)
    expect(isLogEntry({...entry, private_field:'hidden'})).toBe(false)
  })

  it('accepts worker attempt identity in log records', () => {
    expect(isLogEntry({ timestamp:'2026-09-05T08:00:00Z',level:'info',service:'business-worker',module:'worker',message:'event processed',attempt_id:'0102030405060708090a0b0c0d0e0f10',event_id:'123e4567-e89b-12d3-a456-426614174000',event_type:'post.created',attempt:0,result:'success' })).toBe(true)
  })

  it('accepts business stage correlation fields while rejecting invalid values', () => {
    const entry = { timestamp:'2026-09-05T08:00:00Z',level:'info',service:'search-indexer',module:'search',message:'event processed',event_id:'123e4567-e89b-12d3-a456-426614174001',command_id:'123e4567-e89b-42d3-a456-426614174002',data_epoch:'123e4567-e89b-42d3-a456-426614174000',event_type:'post.created',projection_generation:'gopulse-post-search-v1-20261012',routing_contract_revision:'post-created-search/1',consumer:'search',post_id:7,attempt:0,reason:'processed' }
    expect(isLogEntry(entry)).toBe(true)
    expect(isLogEntry({...entry, data_epoch:'not-an-epoch'})).toBe(false)
    expect(isLogEntry({...entry, consumer:'admin'})).toBe(false)
    expect(isLogEntry({...entry, routing_contract_revision:'post-created-search/2'})).toBe(false)
  })

  it('accepts the Phase 20 post commit log message', () => {
    expect(isLogEntry({ timestamp:'2026-09-05T08:00:00Z',level:'info',service:'backend',module:'post',message:'post committed',post_id:9,content_revision:1 })).toBe(true)
  })
  it('rejects impossible metric and event contracts', () => {
    expect(isMetricResult({ metric:'gopulse_redis_up',kind:'gauge',unit:'boolean',range:'15m',from:'2026-09-05T08:15:00Z',to:'2026-09-05T08:00:00Z',step_seconds:999,series:[{labels:{},points:[{timestamp:'2026-09-05T08:10:00Z',value:1},{timestamp:'2026-09-05T08:09:00Z',value:1}]},{labels:{},points:[]}] })).toBe(false)
    expect(isEventEntry({ timestamp:'2026-09-05T08:00:00Z',event_name:'exporter_plugin_started',source:'monitor',severity:'info',message:'arbitrary',metadata:{plugin_id:'redis-exporter',operation:'start'} })).toBe(false)
    expect(isEventEntry({ timestamp:'2026-09-05T08:00:00Z',event_name:'exporter_plugin_started',source:'monitor',severity:'info',message:'exporter plugin started',metadata:{plugin_id:'redis-exporter',plugin_version:'1.8.4',operation:'start',from_state:'stopped',to_state:'running'} })).toBe(true)
  })
  it('sends opaque cursor without replaying filters', async () => {
    const fetchMock=vi.fn().mockResolvedValue(response({data:[],meta:{next_cursor:null}})); vi.stubGlobal('fetch',fetchMock)
    await observabilityApi.logs({range:'15m',service:'backend',module:'http',level:'',message:'',request_id:'',trace_id:'',span_id:'',event_id:'',error_code:'',command_id:'',data_epoch:'',post_id:''},'opaque+/=')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/observability/logs?cursor=opaque%2B%2F%3D')
  })

  it('preserves a bounded stage identity and evidence window on Logs queries', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response({ data: [], meta: { next_cursor: null } })); vi.stubGlobal('fetch', fetchMock)
    await observabilityApi.logs({ range: '15m', service: '', module: '', level: '', message: '', request_id: '', trace_id: '', span_id: '', event_id: '', error_code: '', command_id: '123e4567-e89b-42d3-a456-426614174001', data_epoch: '123e4567-e89b-42d3-a456-426614174000', post_id: '7', from: '2026-10-12T09:45:00Z', to: '2026-10-12T10:00:00Z' })
    const path = fetchMock.mock.calls[0][0] as string
    expect(path).toContain('command_id=123e4567-e89b-42d3-a456-426614174001')
    expect(path).toContain('data_epoch=123e4567-e89b-42d3-a456-426614174000')
    expect(path).toContain('post_id=7')
    expect(path).toContain('from=2026-10-12T09%3A45%3A00Z')
    expect(path).toContain('to=2026-10-12T10%3A00%3A00Z')
  })
})

it('validates the server metric catalog before opening a metric query', async () => {
  const { isMetricCatalog, metricCatalog } = await import('./observability')
  const catalog = metricCatalog.map(item => {
    const component = componentContract(item.value)
    if (component) return {metric:item.value,kind:component.kind,unit:component.unit,source:component.source,target_id:`${component.source}-local`,producer_kind:'component',producer_id:component.source}
    const source = item.value.split('_')[1]
    const unit = (item.value.endsWith('_up') || item.value.endsWith('_controller_available') || item.value.endsWith('_cluster_health_status')) ? 'boolean' : item.value.includes('_seconds') ? 'seconds' : item.value.endsWith('_bytes') ? 'bytes' : 'count'
    return {metric:item.value,kind:item.value.endsWith('_total') ? 'counter' : 'gauge',unit,source,target_id:`${source}-exporter-local`,producer_kind:'exporter_plugin',producer_id:`${source}-exporter`}
  })
  expect(isMetricCatalog(catalog)).toBe(true)
  expect(isMetricCatalog(catalog.map(item => ({...item,producer_id:'mysql-exporter'})))).toBe(false)
})

it('accepts the sixth plugin lifecycle identity without exposing runtime metadata', () => {
  const event = { timestamp:'2026-09-11T08:00:00Z',event_name:'exporter_plugin_started',source:'monitor',severity:'info',message:'exporter plugin started',metadata:{plugin_id:'victoriametrics-exporter',plugin_version:'1.11.4',operation:'start',from_state:'stopped',to_state:'running'} }
  expect(isEventEntry(event)).toBe(true)
  expect(isEventEntry({...event,metadata:{...event.metadata,pid:'123'}})).toBe(false)
})

it('accepts component scraped labels and unknown dependency with additional dimensions', () => {
 const value = {metric:'gopulse_monitor_last_scrape_success_timestamp_seconds',kind:'gauge',unit:'unix_seconds',range:'15m',from:'2026-09-11T08:00:00Z',to:'2026-09-11T08:15:00Z',step_seconds:15,series:[{labels:{scraped_producer_kind:'component',scraped_target_id:'monitor-local'},points:[{timestamp:'2026-09-11T08:15:00Z',value:0}]}]}
 expect(isMetricResult(value)).toBe(true)
 expect(isMetricResult({...value,series:[{...value.series[0],labels:{...value.series[0]!.labels,node:'node-2'}}]})).toBe(true)
 expect(isMetricResult({...value,metric:'gopulse_backend_dependency_up',unit:'state',series:[{labels:{dependency:'redis'},points:[{timestamp:'2026-09-11T08:15:00Z',value:-1}]}]})).toBe(true)
})
