<script setup lang="ts">
import { ref } from 'vue'
import { requestValidatedData, ApiError } from '../services/http'
import { useAuth, leaveManagement } from '../composables/useAuth'
import { isManagedUser, isRoleChange, type ManagedUser as User } from '../services/management'
const id=ref(''),user=ref<User|null>(null),busy=ref(false),message=ref(''),auth=useAuth()
async function lookup(){if(busy.value)return;user.value=null;message.value='';if(!/^[1-9][0-9]*$/.test(id.value)){message.value='请输入精确正整数用户 ID';return}busy.value=true;try{user.value=await requestValidatedData('/admin/users/'+id.value,isManagedUser)}catch{message.value='用户不存在或查询失败'}finally{busy.value=false}}
async function change(){const target=user.value;if(!target||busy.value)return;const role=target.role==='user'?'super_admin':'user';if(!window.confirm(`确认将用户 ${target.id} 的角色变更为 ${role}？`))return;busy.value=true;message.value='';try{const result=await requestValidatedData('/admin/users/'+target.id+'/role',isRoleChange,{method:'PUT',body:JSON.stringify({role})});if(target.id===auth.user.value?.id&&role==='user'){user.value=null;await leaveManagement();return}user.value=result.user;message.value=result.changed?'角色已变更':'角色未改变'}catch(e){message.value=e instanceof ApiError&&e.status===409?'引导超级管理员不可降级':'角色变更失败'}finally{busy.value=false}}
</script>
<template><section><h2>用户角色</h2><form @submit.prevent="lookup"><label>精确用户 ID <input v-model="id" inputmode="numeric" autocomplete="off" /></label><button :disabled="busy">查询用户</button></form><p role="status">{{message}}</p><article v-if="user"><p>ID：{{user.id}} · {{user.username}} · {{user.role}}</p><p>{{new Date(user.created_at).toLocaleString()}} · {{Intl.DateTimeFormat().resolvedOptions().timeZone}}</p><p>引导账号：{{user.is_bootstrap_super_admin?'是':'否'}}</p><button :disabled="busy||user.is_bootstrap_super_admin" @click="change">{{user.role==='user'?'提升为超级管理员':'降级为普通用户'}}</button></article></section></template>
