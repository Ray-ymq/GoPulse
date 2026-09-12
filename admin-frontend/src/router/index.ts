import { createRouter, createWebHistory } from 'vue-router'
import AdminLayout from '../components/AdminLayout.vue'
import Metrics from '../views/ObservabilityMetricsView.vue'
import Logs from '../views/ObservabilityLogsView.vue'
import Events from '../views/ObservabilityEventsView.vue'
import Alerts from '../views/AlertsView.vue'
import Users from '../views/UsersView.vue'
import Audit from '../views/AuditView.vue'
import Dashboard from '../views/DashboardView.vue'
import Plugins from '../views/ObservabilityExportersView.vue'
export const router = createRouter({ history: createWebHistory('/admin/'), routes: [
  { path: '/', component: AdminLayout, children: [
    { path: '', component: Dashboard },
    {path:'alerts',component:Alerts},{path:'users',component:Users},{path:'audit',component:Audit},
    { path: 'metrics', component: Metrics }, { path: 'logs', component: Logs },
    { path: 'events', component: Events }, { path: 'plugins', component: Plugins },
  ] },
  { path: '/:pathMatch(.*)*', redirect: '/metrics' },
] })
