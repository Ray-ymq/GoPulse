import { createApp } from 'vue'
import App from './App.vue'
import { bindUnauthorizedNavigation } from './composables/useAuth'
import { router } from './router'
import { setForbiddenHandler } from './services/http'
import './styles.css'

bindUnauthorizedNavigation(async () => {
  const current = router.currentRoute.value
  if (current.matched.length > 0 && current.path !== '/login') {
    await router.push('/login')
  }
})

setForbiddenHandler(async () => {
  if (router.currentRoute.value.path.startsWith('/admin/')) await router.replace('/forbidden')
})

createApp(App).use(router).mount('#app')
