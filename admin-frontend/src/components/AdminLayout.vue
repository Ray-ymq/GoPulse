<template>
  <div class="admin-shell">
    <header class="admin-header">
      <div><p class="admin-eyebrow">GOPULSE ADMIN</p><h1>可观测中心</h1></div>
      <a class="button button--secondary" href="/posts">返回社交</a>
    </header>
    <nav class="admin-nav" aria-label="可观测导航">
      <RouterLink to="/metrics">Metrics</RouterLink>
      <RouterLink to="/logs">Logs</RouterLink>
      <RouterLink to="/events">Events</RouterLink>
      <RouterLink to="/plugins">Exporter</RouterLink>
    </nav>
    <main class="admin-content"><RouterView /></main>
  </div>
</template>
