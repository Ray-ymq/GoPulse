<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../services/api'
import { errorMessage } from '../services/http'
import { user } from '../composables/session'
import { setFollow } from '../composables/community'
const props = defineProps<{ id: number; following: boolean; name: string }>()
const emit = defineEmits<{ change: [active: boolean] }>()
const busy = ref(false), error = ref('')
async function toggle() { if (busy.value) return; busy.value = true; error.value = ''; const owner = user.value?.id; try { const result = await api.follow(props.id, !props.following); if (user.value?.id === owner) { setFollow(props.id, result.data.following); emit('change', result.data.following) } } catch (e) { error.value = errorMessage(e) } finally { busy.value = false } }
</script>
<template><div><button class="follow" :class="{ active: following }" :disabled="busy" :aria-pressed="following" :aria-label="`${following ? '取消关注' : '关注'}${name}`" @click="toggle">{{ busy ? '处理中' : following ? '已关注' : '+ 关注' }}</button><p v-if="error" class="error" role="alert">{{ error }}</p></div></template>
