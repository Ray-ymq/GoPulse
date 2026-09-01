import { createApp } from 'vue'
import App from './App.vue'
import { bindUnauthorizedNavigation, useAuth } from './composables/useAuth'
import { router } from './router'
import './styles.css'

bindUnauthorizedNavigation(async () => {
  const current = router.currentRoute.value
  if (current.matched.length > 0 && current.path !== '/login') {
    await router.push('/login')
  }
})

void useAuth().initialize()
createApp(App).use(router).mount('#app')
