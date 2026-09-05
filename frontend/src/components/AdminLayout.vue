<template>
  <div class="admin-shell">
    <header class="admin-header">
      <div><p class="admin-eyebrow">GOPULSE ADMIN</p><h1>可观测中心</h1></div>
      <RouterLink class="button button--secondary" to="/posts">返回社交</RouterLink>
    </header>
    <nav class="admin-nav" aria-label="可观测导航">
      <RouterLink to="/admin/observability/metrics">Metrics</RouterLink>
      <RouterLink to="/admin/observability/logs">Logs</RouterLink>
      <RouterLink to="/admin/observability/events">Events</RouterLink>
      <span class="admin-nav__disabled" aria-disabled="true">Exporter（下一批）</span>
    </nav>
    <main class="admin-content"><RouterView /></main>
  </div>
</template>
