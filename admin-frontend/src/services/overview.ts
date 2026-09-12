import { requestData } from './http'
export type Section = { status: string; observed_at: string | null; reason_code: string; items: Record<string, unknown>[] | Record<string, unknown> }
export type Overview = { generated_at: string; status: string; sections: Record<string, Section> }
export const sectionNames = ['components', 'key_metrics', 'logs', 'events', 'plugins', 'alerts'] as const
const fields: Record<string,string[]> = {
 components: ['id','status','observed_at','reason_code'], key_metrics: ['id','value','unit','observed_at','status','reason_code'],
 logs:['severity','value','status'],events:['severity','value','status'],plugins:['id','installed','desired','observed','up','observed_at','status','reason_code'],
 alerts:['recoveries','warning_firing','critical_firing','pending','unknown','recently_recovered','evaluator_enabled','last_success_at'],
}
const statuses = ['healthy','degraded','unavailable','unknown']
const reasons = ['ok','partial_data','upstream_unavailable','missing','stale','dependency_down','monitor_unavailable','not_installed','process_failed']
function record(v: unknown): v is Record<string,unknown> { return !!v && typeof v === 'object' && !Array.isArray(v) }
function exact(v: Record<string,unknown>, keys: string[]) { return Object.keys(v).length === keys.length && keys.every(k => k in v) }
function item(v: unknown, name: string): v is Record<string,unknown> {
 if (!record(v) || !exact(v,fields[name]!)) return false
 return Object.entries(v).every(([k,x]) => {
  if (['observed_at','last_success_at'].includes(k)) return x === null || typeof x === 'string' && Number.isFinite(Date.parse(x))
  if (k === 'status') return statuses.includes(String(x))
  if (k === 'reason_code') return reasons.includes(String(x))
  if (['value','up'].includes(k)) return x === null || typeof x === 'number' && Number.isFinite(x)
  if (k === 'recoveries') return Array.isArray(x) && x.length<=5 && x.every(r=>record(r)&&exact(r,['rule_id','revision','severity','recovered_at'])&&Number.isSafeInteger(r.rule_id)&&Number(r.rule_id)>0&&Number.isSafeInteger(r.revision)&&Number(r.revision)>0&&['warning','critical'].includes(String(r.severity))&&typeof r.recovered_at==='string'&&Number.isFinite(Date.parse(r.recovered_at)))
  if (k === 'installed') return x === null || typeof x === 'boolean'
  if (k === 'evaluator_enabled') return typeof x === 'boolean'
  if (['warning_firing','critical_firing','pending','unknown','recently_recovered'].includes(k)) return Number.isSafeInteger(x) && Number(x)>=0
  if (k === 'unit') return x === 'count'
  if (k === 'severity') return ['info','warn','error'].includes(String(x))
  if (['desired','observed'].includes(k)) return ['unknown','running','stopped','failed','starting','stopping','installed'].includes(String(x))
  if (k === 'id') return typeof x === 'string' && /^(gopulse_(backend_outbox_pending|business_worker_messages_in_flight|search_indexer_retrying|monitor_event_queue_length|router_buffered_records|marshaller_retrying)|backend|business-worker|search-indexer|monitor|router|marshaller|(redis|mysql|kafka|elasticsearch|rabbitmq|victoriametrics)-exporter)$/.test(x)
  return false
 })
}
export function parseOverview(raw: unknown): Overview {
 if (!record(raw) || !exact(raw,['generated_at','status',...sectionNames]) || typeof raw.generated_at !== 'string' || !Number.isFinite(Date.parse(raw.generated_at)) || !['healthy','degraded','unavailable'].includes(String(raw.status))) throw new Error('大屏响应无效')
 const sections: Record<string,Section> = {}
 for (const name of sectionNames) {
  const v = raw[name]
  if (record(v) && exact(v,['status','observed_at','reason_code','items']) && statuses.includes(String(v.status)) && reasons.includes(String(v.reason_code)) && (v.observed_at === null || typeof v.observed_at === 'string' && Number.isFinite(Date.parse(v.observed_at))) && (name === 'alerts' ? item(v.items,name) || v.status === 'unavailable' && Array.isArray(v.items) && !v.items.length : Array.isArray(v.items) && v.items.every(x=>item(x,name)))) sections[name] = v as Section
  else sections[name] = {status:'unavailable',observed_at:null,reason_code:'invalid_response',items:[]}
 }
 return {generated_at:raw.generated_at,status:String(raw.status),sections}
}
export async function getOverview() { return parseOverview(await requestData<unknown>('/admin/overview?range=15m')) }
