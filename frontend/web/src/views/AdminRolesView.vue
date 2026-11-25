<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import InkDialog from '../components/InkDialog.vue'
import { user } from '../composables/session'
import { api } from '../services/api'
import { ApiError, errorMessage } from '../services/http'
import type { ManagedUser, User } from '../types/api'

const router = useRouter()
const username = ref(''), account = ref<ManagedUser | null>(null)
const busy = ref(false), confirmation = ref(false), error = ref(''), message = ref('')
const nextRole = computed<User['role']>(() => account.value?.role === 'super_admin' ? 'user' : 'super_admin')
const action = computed(() => nextRole.value === 'super_admin' ? '设为超级管理员' : '撤销超级管理员权限')
const permitted = computed(() => user.value?.role === 'super_admin')

async function handleError(cause: unknown) {
  error.value = errorMessage(cause)
  if (cause instanceof ApiError && cause.status === 403) {
    account.value = null
    try { user.value = (await api.me()).data } catch { /* 保留原始权限错误，允许用户重新加载页面。 */ }
  }
}

async function lookup() {
  if (busy.value) return
  busy.value = true; error.value = ''; message.value = ''; account.value = null
  try { account.value = (await api.managedUser(username.value.trim())).data }
  catch (cause) { await handleError(cause) }
  finally { busy.value = false }
}

async function save() {
  if (busy.value || !account.value || !permitted.value) return
  const target = account.value, role = nextRole.value
  busy.value = true; error.value = ''; message.value = ''
  try {
    const result = (await api.changeRole(target.id, role)).data
    account.value = result.user
    message.value = result.changed ? '角色已更新。' : '该账号已具有所选角色，无需更改。'
    if (target.id === user.value?.id) {
      user.value = { ...user.value, role: result.user.role }
      if (result.user.role !== 'super_admin') await router.replace('/')
    }
  } catch (cause) {
    await handleError(cause)
    if (!(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') {
      account.value = null
      error.value = '未能确认角色变更结果。请重新查找该账号，核对当前角色后再操作。'
    }
  } finally { busy.value = false; confirmation.value = false }
}
</script>

<template>
  <section class="role-management">
    <div class="section-head"><div class="section-title"><h1>角色管理</h1></div></div>
    <p class="hint">按完整用户名查找账号，查看并设置其角色。</p>
    <form v-if="permitted" class="role-lookup" @submit.prevent="lookup">
      <label class="form-field"><span>用户名</span><input v-model="username" required minlength="3" maxlength="32" pattern="[A-Za-z0-9_]{3,32}" autocomplete="off" :disabled="busy" placeholder="输入完整用户名" /></label>
      <button class="primary" :disabled="busy">{{ busy ? '正在处理…' : '查找账号' }}</button>
    </form>
    <p v-else class="hint">当前账号没有角色管理权限。</p>
    <p v-if="error" class="inline-error" role="alert">{{ error }}</p>
    <p v-if="message" class="hint" role="status">{{ message }}</p>
    <article v-if="account && permitted" class="post role-account">
      <h2>{{ account.username }}</h2>
      <p>用户 ID：{{ account.id }}</p>
      <p>当前角色：<strong>{{ account.role === 'super_admin' ? '超级管理员' : '普通用户' }}</strong></p>
      <p v-if="account.is_bootstrap_super_admin" class="hint">这是初始超级管理员，不能撤销其权限。</p>
      <button v-else class="secondary" :disabled="busy" @click="confirmation = true">{{ action }}</button>
    </article>
    <InkDialog :open="confirmation" :title="`${action}？`" label-id="role-confirm-title" @close="confirmation = false">
      <p>账号：{{ account?.username }}（ID：{{ account?.id }}）</p>
      <p v-if="nextRole === 'super_admin'" class="hint">该账号将能管理其他用户的角色。</p>
      <p v-else class="hint">该账号将失去超级管理员权限，保留普通用户功能。</p>
      <p v-if="account?.id === user?.id && nextRole === 'user'" class="hint">你正在撤销自己的权限。完成后将返回首页。</p>
      <div class="inline-actions"><button class="secondary" :disabled="busy" @click="confirmation = false">取消</button><button class="primary" :disabled="busy" @click="save">{{ busy ? '正在保存…' : '确认更改' }}</button></div>
    </InkDialog>
  </section>
</template>

<style scoped>
.role-lookup { display: grid; gap: 16px; margin: 24px 0; }
.role-lookup .primary { justify-self: start; }
.role-account { margin-top: 24px; overflow-wrap: anywhere; }
.role-account h2 { margin-top: 0; }
</style>
