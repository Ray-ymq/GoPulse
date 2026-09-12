import { createRouter, createWebHistory } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
import Metrics from '../views/ObservabilityMetricsView.vue'
import Logs from '../views/ObservabilityLogsView.vue'
import Events from '../views/ObservabilityEventsView.vue'
import Plugins from '../views/ObservabilityExportersView.vue'
export const router = createRouter({ history: createWebHistory('/admin/'), routes: [
  { path: '/', component: AdminLayout, children: [
    { path: '', redirect: '/metrics' },
    { path: 'metrics', component: Metrics }, { path: 'logs', component: Logs },
    { path: 'events', component: Events }, { path: 'plugins', component: Plugins },
  ] },
  { path: '/:pathMatch(.*)*', redirect: '/metrics' },
] })
