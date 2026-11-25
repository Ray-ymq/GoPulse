import { computed, ref } from 'vue'
import { api } from '../services/api'
import type { Post } from '../types/api'
import { user } from './session'
import { hashtags } from '../utils/format'
export const latestPosts = ref<Post[]>([])
export const contentVersion = ref(0)
export const unreadCount = ref(0)
export const people = computed(() => [...new Map(latestPosts.value.map(p => [p.author.id, p.author])).values()])
export const topics = computed(() => [...new Set(latestPosts.value.flatMap(p => hashtags(p.title + ' ' + p.content)))].slice(0, 4))
export async function refreshCommunity() { const id = user.value?.id; const result = await api.posts(); if (user.value?.id === id) latestPosts.value = result.data }
export async function refreshNotifications() { const id = user.value?.id; const result = await api.notifications(); if (user.value?.id === id) unreadCount.value = result.data.filter(n => !n.read_at).length }
export function updatePost(post: Post) { latestPosts.value = latestPosts.value.map(p => p.id === post.id ? post : p) }
export function setFollow(id: number, active: boolean) { latestPosts.value.forEach(p => { if (p.author.id === id) p.author.following = active }); contentVersion.value++ }
export function resetCommunity() { latestPosts.value = []; unreadCount.value = 0; contentVersion.value++ }
