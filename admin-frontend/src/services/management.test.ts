import { afterEach, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { isCatalog, isRule, isIncident, isManagedUser, isRoleChange, isAuditEvent } from './management'
import UsersView from '../views/UsersView.vue'
import AlertsView from '../views/AlertsView.vue'
import AuditView from '../views/AuditView.vue'
const at='2026-09-13T00:00:00Z'
const user={id:2,username:'review-user',role:'user',created_at:at,is_bootstrap_super_admin:false}
const count={fields:{level:['error']},allowed_combinations:[{level:'error'}],reducers:['count'],minimum_selectors:1}
const catalog={metrics:[{metric:'gopulse_redis_up',kind:'gauge',unit:'state',label_keys:[],allowed_tuples:[[]],reducers:['last']}],logs:count,events:{...count,fields:{severity:['error']},allowed_combinations:[{severity:'error'}]},creatable_sources:['metrics','logs','events'],operators:['gt'],windows:['1m'],for:['0s'],severities:['warning']}
const rule={id:1,name:'review-rule',enabled:true,severity:'warning',source:'metrics',selector:{metric:'gopulse_redis_up',labels:{}},reducer:'last',operator:'gt',threshold:0,window:'1m',for:'0s',revision:1,created_by:1,updated_by:1,created_at:at,updated_at:at,evaluation:{state:'normal',data_status:'unknown',pending_since:null,active_incident_id:null,last_value:null,last_evaluated_at:null,last_success_at:null,error_code:''}}
const incident={id:1,rule_id:1,revision:1,name:'review-rule',severity:'warning',source:'metrics',object:rule.selector,status:'firing',data_status:'ok',first_triggered_at:at,last_triggered_at:at,last_evaluated_at:at,recovered_at:null,closed_at:null,resolution_reason:'',last_value:1,evaluation_count:1}
const audit={id:1,operation_id:'review-op',occurred_at:at,actor_kind:'user',actor_user_id:1,action:'user.role.change',resource_type:'user',resource_id:'2',phase:'completed',outcome:'succeeded',request_id:'review-request',details_json:{before:'user',after:'super_admin'}}
afterEach(()=>{vi.unstubAllGlobals();vi.restoreAllMocks()})
for(const [name,validate,value,bad] of [
 ['catalog',isCatalog,catalog,{...catalog,operators:['execute']}],
 ['rule',isRule,rule,{...rule,evaluation:{...rule.evaluation,state:'healthy'}}],
 ['incident',isIncident,incident,{...incident,object:{...incident.object,secret:'hidden'}}],
 ['user',isManagedUser,user,{...user,role:'admin'}],
 ['role change',isRoleChange,{user,changed:true},{user,changed:'true'}],
 ['audit',isAuditEvent,audit,{...audit,details_json:{...audit.details_json,secret:'hidden'}}],
] as const){it(`${name} accepts its DTO and rejects missing, extra and illegal fields`,()=>{
 expect(validate(value)).toBe(true)
 expect(validate({...value,unexpected:'hidden'})).toBe(false)
 const missing={...value};delete (missing as Record<string,unknown>)[Object.keys(missing)[0]!]
 expect(validate(missing)).toBe(false);expect(validate(bad)).toBe(false)
})}
it('accepts the bounded rule, alert and plugin audit detail variants',()=>{
 const details={rule_id:1,revision:1,severity:'warning',source:'events'}
 for(const [action,resource_type,details_json] of [['rule.update','rule',details],['alert.close','alert',{rule:details,reason_code:'rule_updated'}],['plugin.start','plugin',{plugin_id:'redis-exporter'}],['plugin.update','plugin',{plugin_id:'redis-exporter',version:'1.12.7',reason_code:'conflict'}]]){
 expect(isAuditEvent({...audit,action,resource_type,details_json})).toBe(true)
 }
})
it('does not show user controls for the reported legacy role and extra-field response',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({data:{...user,role:'admin',unexpected:'hidden'}}))))
 const w=mount(UsersView);await w.get('input').setValue('2');await w.get('form').trigger('submit');await flushPromises()
 expect(w.find('article').exists()).toBe(false);w.unmount()
})
it('accepts lookup and role-change results but never applies malformed mutation data',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({data:user}))).mockResolvedValueOnce(new Response(JSON.stringify({data:{user:{...user,role:'super_admin'},changed:true}}))).mockResolvedValueOnce(new Response(JSON.stringify({data:{user:{...user,username:'unsafe',role:'admin'},changed:true}})))
 vi.stubGlobal('fetch',fetch);vi.spyOn(window,'confirm').mockReturnValue(true)
 const w=mount(UsersView);await w.get('input').setValue('2');await w.get('form').trigger('submit');await flushPromises()
 expect(w.get('article').text()).toContain('review-user')
 await w.get('article button').trigger('click');await flushPromises();expect(w.get('article').text()).toContain('super_admin')
 await w.get('article button').trigger('click');await flushPromises();expect(w.text()).not.toContain('unsafe');expect(w.text()).toContain('角色变更失败');w.unmount()
})
it('validates alert catalog, list and mutation responses through the actual page',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({data:catalog}))).mockResolvedValueOnce(new Response(JSON.stringify({data:[rule],meta:{next_cursor:null}})))
 vi.stubGlobal('fetch',fetch)
 const w=mount(AlertsView);await flushPromises();expect(w.text()).toContain('review-rule')
 // An enable/disable mutation must not report success for an invalid rule DTO.
 vi.spyOn(window,'confirm').mockReturnValue(true)
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({data:{...rule,source:'invalid'}})))
 const button=w.findAll('button').find(b=>b.text()==='停用')!;await button.trigger('click');await flushPromises()
 expect(w.text()).toContain('操作失败');expect(w.text()).not.toContain('操作成功');w.unmount()
})
it('rejects malformed audit details without displaying them',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({data:[{...audit,details_json:{secret:'must-not-render'}}],meta:{next_cursor:null}}))))
 const w=mount(AuditView);await flushPromises();expect(w.text()).toContain('审计查询失败');expect(w.text()).not.toContain('must-not-render');w.unmount()
})
