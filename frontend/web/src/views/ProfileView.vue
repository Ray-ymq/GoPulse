<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../services/api'
import { errorMessage } from '../services/http'
import { user, myProfile } from '../composables/session'
import { contentVersion } from '../composables/community'
import type { Profile } from '../types/api'
import { displayName, validText } from '../utils/format'
import InkAvatar from '../components/InkAvatar.vue'
import FollowButton from '../components/FollowButton.vue'
import PostFeed from '../components/PostFeed.vue'
import InkDialog from '../components/InkDialog.vue'
const route = useRoute(), profile = ref<Profile | null>(null), loading = ref(false), error = ref(''), edit = ref(false), saving = ref(false), name = ref(''), bio = ref(''), editError = ref('')
const username = computed(() => typeof route.params.username === 'string' ? route.params.username : user.value?.username || '')
async function load() { loading.value = true; error.value = ''; try { profile.value = (await api.profile(username.value)).data; if (profile.value.is_self && user.value?.id === profile.value.id) myProfile.value = profile.value } catch (e) { error.value = errorMessage(e) } finally { loading.value = false } }
function openEdit() { if (!profile.value) return; name.value = profile.value.display_name || profile.value.username; bio.value = profile.value.bio; editError.value = ''; edit.value = true }
async function save() { if (saving.value) return; if (!validText(name.value, 64) || Array.from(bio.value.trim()).length > 160) { editError.value = '显示名称需为 1–64 个字符，简介最多 160 个字符。'; return }; saving.value = true; editError.value = ''; try { profile.value = (await api.updateProfile(name.value.trim(), bio.value.trim())).data; myProfile.value = profile.value; contentVersion.value++; edit.value = false } catch (e) { editError.value = errorMessage(e) } finally { saving.value = false } }
onMounted(load)
</script>
<template><div v-if="loading" class="state-block" role="status">正在展开个人主页……</div><div v-if="error" class="state-block" role="alert"><p>{{ error }}</p><button class="secondary" @click="load">重新加载</button></div><p v-if="route.query.published === 'unavailable'" class="app-notice" role="status">发布结果已确认，但相关内容目前不可读取。</p><template v-if="profile"><section class="page-banner"><div class="profile-top"><InkAvatar :name="displayName(profile)" /><div><h1>{{ displayName(profile) }}</h1><p>@{{ profile.username }}</p></div></div><p class="profile-bio">{{ profile.bio || '还没有写下个人简介。' }}</p><div class="profile-tools"><template v-if="profile.is_self"><button class="secondary" @click="openEdit">编辑资料</button><RouterLink class="link-button" to="/relations/following">我的关注</RouterLink><RouterLink class="link-button" to="/relations/followers">关注我的人</RouterLink></template><FollowButton v-else :id="profile.id" :name="displayName(profile)" :following="profile.following" @change="profile.following = $event" /></div></section><div class="section-head"><div class="section-title"><h2>{{ profile.is_self ? '我的分享' : '最近分享' }}</h2></div><span class="hint">按发布时间</span></div><PostFeed :endpoint="`/users/${encodeURIComponent(profile.username)}/posts`" empty-title="故事，从这里开始" empty-text="这里还没有发布过的分享。" /></template><InkDialog :open="edit" title="写下你的样子" label-id="profile-edit-title" @close="edit = false"><form @submit.prevent="save"><label class="form-field"><span>显示名称</span><input v-model="name" required maxlength="128" :disabled="saving" /></label><label class="form-field"><span>个人简介</span><textarea v-model="bio" maxlength="320" :disabled="saving" /></label><p v-if="editError" class="inline-error" role="alert">{{ editError }}</p><button class="primary" :disabled="saving">{{ saving ? '正在保存…' : '保存资料' }}</button></form></InkDialog></template>
