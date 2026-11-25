<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { user } from '../composables/session'
import { contentVersion, refreshCommunity } from '../composables/community'
import { errorMessage } from '../services/http'
import { pendingPost, publish, resolvePost } from '../services/post-command'
import type { CommandResult } from '../types/api'
import { validText } from '../utils/format'
import InkDialog from './InkDialog.vue'
import Icon from './Icon.vue'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const router = useRouter(), title = ref(''), content = ref(''), error = ref(''), busy = ref(false), unresolved = ref(false)
let owner = 0
watch([() => props.open, () => user.value?.id], () => {
  const id = user.value?.id || 0
  if (id !== owner) { title.value = ''; content.value = ''; owner = id }
  if (!props.open || !id) return
  const pending = pendingPost(id); unresolved.value = !!pending; error.value = ''
  if (pending) { title.value = pending.input.title; content.value = pending.input.content }
  else { try { const draft = JSON.parse(localStorage.getItem(`gopulse.draft.${id}`) || 'null'); if (draft && typeof draft.title === 'string' && typeof draft.content === 'string') { title.value = draft.title; content.value = draft.content } } catch { /* 草稿存储不可用时保留当前输入。 */ } }
})
watch([title, content], () => { if (owner && !unresolved.value) { try { localStorage.setItem(`gopulse.draft.${owner}`, JSON.stringify({ title: title.value, content: content.value })) } catch { /* 输入仍保留在当前页面。 */ } } })
async function completed(result: CommandResult) {
  title.value = ''; content.value = ''; unresolved.value = false
  try { localStorage.removeItem(`gopulse.draft.${owner}`) } catch { /* 发布结果已经确认，不依赖草稿存储。 */ }
  contentVersion.value++; void refreshCommunity().catch(() => {})
  emit('close'); await router.push(result.data ? `/posts/${result.data.id}` : '/me?published=unavailable')
}
async function submit(check = false) {
  if (!user.value || busy.value) return
  if (!check && (!validText(title.value, 120) || !validText(content.value, 10000))) { error.value = '标题需为 1–120 个字符，正文需为 1–10000 个字符，不能只有空白。'; return }
  const ownerId = user.value.id
  busy.value = true; error.value = ''
  try { const result = check ? await resolvePost(ownerId) : await publish(ownerId, { title: title.value.trim(), content: content.value.trim() }); if (user.value?.id === ownerId) await completed(result) }
  catch (e) { error.value = errorMessage(e); unresolved.value = !!pendingPost(ownerId) }
  finally { busy.value = false }
}
</script>
<template><InkDialog :open="open" title="分享你的新发现" label-id="composer-title" @close="emit('close')"><form @submit.prevent="submit()"><p v-if="unresolved" class="app-notice">上次发布的结果尚未确认。请先查证结果，或使用原内容重试，避免重复发布。</p><label class="form-field"><span>帖子标题</span><input v-model="title" :readonly="unresolved" :disabled="busy" required maxlength="240" placeholder="给这次分享起个名字" /></label><label class="form-field"><span>分享内容</span><textarea v-model="content" :readonly="unresolved" :disabled="busy" required maxlength="20000" placeholder="一个想法、一段经历，或是今天的小小发现……" /></label><p class="hint">{{ Array.from(content.trim()).length }} / 10000 字符 · 可在正文中使用 #话题</p><p v-if="error" class="inline-error" role="alert">{{ error }}</p><div v-if="unresolved" class="inline-actions"><button type="button" class="secondary" :disabled="busy" @click="submit(true)">{{ busy ? '正在查证…' : '查证发布结果' }}</button></div><div class="form-row"><span class="hint">草稿保存在当前浏览器。<br>关闭窗口后可继续编辑。</span><button class="primary" :disabled="busy">{{ busy ? '正在处理…' : unresolved ? '重试原发布' : '发布帖子' }}<Icon name="arrow" /></button></div></form></InkDialog></template>
