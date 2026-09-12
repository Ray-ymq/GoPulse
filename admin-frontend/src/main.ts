import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { bindAuthNavigation } from './composables/useAuth'
import './styles.css'
bindAuthNavigation()
createApp(App).use(router).mount('#app')
