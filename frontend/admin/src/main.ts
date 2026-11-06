import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { bindAuthNavigation } from './composables/useAuth'
import './styles.css'
import { loadDeployment } from './composables/deployment'
void loadDeployment().finally(() => {
  bindAuthNavigation()
  createApp(App).use(router).mount('#app')
})
