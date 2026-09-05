import { afterEach, describe, expect, it, vi } from 'vitest'
import { exporterApi, isExporterStatus, validateExporterPackage } from './exporters'

const status = { id:'redis-exporter',name:'Redis Exporter',version:'1.8.2',kind:'metrics-exporter',source:'redis',desired_state:'running',observed_state:'running',installed_at:'2026-09-05T08:00:00Z',updated_at:'2026-09-05T08:01:00Z',started_at:'2026-09-05T08:00:10Z',last_scrape_at:'2026-09-05T08:01:00Z',last_success_at:'2026-09-05T08:01:00Z' }
afterEach(() => vi.unstubAllGlobals())
describe('exporter runtime boundary', () => {
  it('accepts the fixed public status and rejects unsafe data', () => {
    expect(isExporterStatus(status)).toBe(true)
    expect(isExporterStatus({ ...status, pid: 123 })).toBe(false)
    expect(isExporterStatus({ ...status, started_at: null })).toBe(false)
    expect(isExporterStatus({ ...status, last_error:{code:'private',message:'/tmp/secret',at:'2026-09-05T08:02:00Z'} })).toBe(false)
  })
  it('uses browser multipart without manually setting a boundary', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({data:status}), {status:201,headers:{'Content-Type':'application/json'}})); vi.stubGlobal('fetch',fetchMock)
    const file = new File(['package'], 'redis.tar.gz', {type:'application/gzip'})
    await exporterApi.install(file)
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.body).toBeInstanceOf(FormData)
    expect(new Headers(init.headers).has('Content-Type')).toBe(false)
  })
  it('validates package extension, content, and size', () => {
    expect(validateExporterPackage(null)).toContain('请选择')
    expect(validateExporterPackage(new File([], 'empty.tar.gz'))).toContain('不能为空')
    expect(validateExporterPackage(new File(['x'], 'plugin.zip'))).toContain('.tar.gz')
    expect(validateExporterPackage(new File(['x'], 'plugin.tar.gz'))).toBe('')
  })
})
