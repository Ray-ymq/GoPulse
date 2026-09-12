<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getOverview, sectionNames, type Overview } from '../services/overview'
const data=ref<Overview|null>(null),busy=ref(false),error=ref(false)
const links:Record<string,string>={components:'/metrics',key_metrics:'/metrics',logs:'/logs',events:'/events',plugins:'/plugins',alerts:'/alerts'}
const titles:Record<string,string>={components:'六组件',key_metrics:'关键指标',logs:'日志 · 15 分钟',events:'事件 · 15 分钟',plugins:'六插件',alerts:'告警概览'}
const timezone=Intl.DateTimeFormat().resolvedOptions().timeZone
function display(v:unknown) { if(Array.isArray(v))return v.length?v.map(r=>`rule ${r.rule_id} / revision ${r.revision} · ${r.severity} · ${new Date(r.recovered_at).toLocaleString()}`).join('；'):'暂无近期恢复'; return v===null?'未知':typeof v==='string'&&/^\d{4}-\d\d-\d\dT/.test(v)?new Date(v).toLocaleString():String(v) }
async function load(){if(busy.value)return;busy.value=true;error.value=false;data.value=null;try{data.value=await getOverview()}catch{error.value=true}finally{busy.value=false}}
onMounted(load)
</script>
<template><section><h2>管理大屏</h2><button :disabled="busy" @click="load">刷新大屏</button><p v-if="busy" role="status">正在加载大屏…</p><p v-if="error" role="alert">大屏加载失败，请重试。</p><template v-if="data"><p>生成时间：{{new Date(data.generated_at).toLocaleString()}} · {{timezone}} · {{data.status}}</p><div class="dashboard-grid"><article v-for="name in sectionNames" :key="name" class="dashboard-section" :data-section="name"><h3>{{titles[name]}}</h3><p>{{data.sections[name]!.status}} · {{data.sections[name]!.reason_code}}</p><p v-if="data.sections[name]!.reason_code==='invalid_response'">该分区响应无效，已隐藏未经验证的数据。</p><template v-else><template v-if="Array.isArray(data.sections[name]!.items)"><p v-if="!data.sections[name]!.items.length">暂无可用数据</p><dl v-for="(row,i) in data.sections[name]!.items" :key="i"><template v-for="(v,k) in row" :key="k"><dt>{{k}}</dt><dd>{{display(v)}}</dd></template></dl></template><dl v-else><template v-for="(v,k) in data.sections[name]!.items" :key="k"><dt>{{k}}</dt><dd>{{display(v)}}</dd></template></dl><p v-if="name==='logs'||name==='events'">计数 0 表示此时间窗无记录；未知不代表 0。</p><p v-if="name==='alerts'">firing 为 0 表示无当前告警；unknown 不代表已恢复。</p></template><RouterLink :to="links[name]!">查看详情</RouterLink></article></div></template></section></template>
