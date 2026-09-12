import { describe,it,expect } from 'vitest'
import { parseOverview,sectionNames } from './overview'
describe('overview trust boundary',()=>{
 it('isolates an extra section field and never renders its value',()=>{
 const section={status:'unavailable',observed_at:null,reason_code:'upstream_unavailable',items:[]}
 const raw={generated_at:'2026-09-12T00:00:00Z',status:'degraded',...Object.fromEntries(sectionNames.map(n=>[n,section])),logs:{...section,secret:'must-not-render'}}
 const result=parseOverview(raw)
 expect(result.sections.logs?.reason_code).toBe('invalid_response')
 expect(result.sections.events?.reason_code).toBe('upstream_unavailable')
 expect(JSON.stringify(result)).not.toContain('must-not-render')
 })
})
