// Runtime schemas mirror the bounded management DTOs; no unknown fields enter UI state.
type Check<T = unknown> = (value: unknown) => value is T
type Value<C> = C extends Check<infer T> ? T : never
const text: Check<string> = (v): v is string => typeof v === 'string'
const bool: Check<boolean> = (v): v is boolean => typeof v === 'boolean'
const number: Check<number> = (v): v is number => typeof v === 'number' && Number.isFinite(v)
const id: Check<number> = (v): v is number => number(v) && Number.isSafeInteger(v) && v > 0
const integer: Check<number> = (v): v is number => number(v) && Number.isSafeInteger(v) && v >= 0
const date: Check<string> = (v): v is string => text(v) && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(v) && Number.isFinite(Date.parse(v))
const record = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const choice = <T extends string>(...values: T[]): Check<T> => (v): v is T => text(v) && values.includes(v as T)
const nullable = <T>(check: Check<T>): Check<T | null> => (v): v is T | null => v === null || check(v)
const array = <T>(check: Check<T>): Check<T[]> => (v): v is T[] => Array.isArray(v) && v.every(check)
const shape = <S extends Record<string, Check>>(fields: S): Check<{[K in keyof S]: Value<S[K]>}> => (v): v is {[K in keyof S]: Value<S[K]>} => record(v) && Object.keys(v).length === Object.keys(fields).length && Object.entries(fields).every(([k, check]) => Object.hasOwn(v, k) && check(v[k]))
const dictionary = <T>(keys: string[], check: Check<T>): Check<Record<string,T>> => (v): v is Record<string,T> => record(v) && Object.entries(v).every(([k,x]) => keys.includes(k) && check(x))
const source = choice('metrics','logs','events'), severity = choice('warning','critical'), role = choice('user','super_admin')
const reducer = choice('last','min','max','avg','increase','count'), operator = choice('gt','gte','lt','lte','eq','neq')
const window = choice('1m','5m','15m'), duration = choice('0s','1m','5m')
const reason = choice('','rule_updated','rule_disabled','rule_deleted')
const labelKeys = ['source','target','producer','severity','event_type','plugin_id','operation','outcome','reason_code','component','instance','job','mode','result','state','status','db','dependency','method','route','status_class','scraped_producer_kind','scraped_target_id','type','message_source','stage','storage','level','error_code','service','module','message','event_name']
const labels = dictionary(labelKeys, text)
const selector = shape({metric:text,labels})
const countCatalog = shape({fields:dictionary(labelKeys,array(text)),allowed_combinations:array(labels),reducers:array(choice('count')),minimum_selectors:integer})
export const isCatalog = shape({metrics:array(shape({metric:text,kind:choice('gauge','counter'),unit:text,label_keys:array(choice(...labelKeys)),allowed_tuples:array(array(text)),reducers:array(reducer)})),logs:countCatalog,events:countCatalog,creatable_sources:array(source),operators:array(operator),windows:array(window),for:array(duration),severities:array(severity)})
export const isRule = shape({name:text,enabled:bool,severity,source,selector,reducer,operator,threshold:number,window,for:duration,revision:id,id,created_by:id,updated_by:id,created_at:date,updated_at:date,evaluation:shape({state:choice('normal','pending','firing','disabled'),data_status:choice('ok','unknown','stale'),pending_since:nullable(date),active_incident_id:nullable(id),last_value:nullable(number),last_evaluated_at:nullable(date),last_success_at:nullable(date),error_code:choice('','metrics_unknown','logs_unknown','events_unknown')})})
export const isIncident = shape({data_status:choice('ok','unknown','stale','historical'),id,rule_id:id,revision:id,name:text,severity,source,object:selector,status:choice('firing','recovered','closed'),first_triggered_at:date,last_triggered_at:date,last_evaluated_at:date,recovered_at:nullable(date),closed_at:nullable(date),resolution_reason:reason,last_value:nullable(number),evaluation_count:integer})
export const isManagedUser = shape({id,username:text,role,created_at:date,is_bootstrap_super_admin:bool})
export const isRoleChange = shape({user:isManagedUser,changed:bool})
const ruleDetails = shape({rule_id:id,revision:id,severity,source})
const pluginID = choice('redis-exporter','mysql-exporter','kafka-exporter','elasticsearch-exporter','rabbitmq-exporter','victoriametrics-exporter')
const pluginDetails: Check<Record<string,unknown>> = (v): v is Record<string,unknown> => record(v) && pluginID(v.plugin_id) && Object.keys(v).every(k=>['plugin_id','version','reason_code'].includes(k)) && (!Object.hasOwn(v,'version') || text(v.version) && /^[0-9]{1,9}\.[0-9]{1,9}\.[0-9]{1,9}$/.test(v.version)) && (!Object.hasOwn(v,'reason_code') || choice('validation_failed','operation_failed','unavailable','conflict')(v.reason_code))
const alertDetails: Check<Record<string,unknown>> = (v): v is Record<string,unknown> => record(v) && ruleDetails(v.rule) && Object.keys(v).every(k=>['rule','reason_code'].includes(k)) && (!Object.hasOwn(v,'reason_code') || reason(v.reason_code))
const auditShape = shape({id,operation_id:text,occurred_at:date,actor_kind:choice('user','system'),actor_user_id:nullable(id),action:choice('bootstrap.declare','user.role.change','rule.create','rule.update','rule.enable','rule.disable','rule.delete','plugin.connection-test','plugin.install','plugin.configuration','plugin.start','plugin.stop','plugin.update','alert.trigger','alert.recover','alert.close'),resource_type:choice('user','rule','plugin','alert'),resource_id:text,phase:choice('requested','completed'),outcome:choice('succeeded','failed','unknown'),request_id:text,details_json:((v): v is Record<string,unknown> => record(v))})
export const isAuditEvent: typeof auditShape = (v): v is Value<typeof auditShape> => {
 if (!auditShape(v)) return false
 if (v.action.startsWith('rule.')) return v.resource_type === 'rule' && ruleDetails(v.details_json)
 if (v.action.startsWith('plugin.')) return v.resource_type === 'plugin' && pluginDetails(v.details_json)
 if (v.action.startsWith('alert.')) return v.resource_type === 'alert' && alertDetails(v.details_json)
 return v.resource_type === 'user' && shape({before:role,after:role})(v.details_json)
}
export type Catalog = Value<typeof isCatalog>
export type Rule = Value<typeof isRule>
export type Incident = Value<typeof isIncident>
export type ManagedUser = Value<typeof isManagedUser>
export type AuditEvent = Value<typeof isAuditEvent>
