<template>
  <div class="admin-shell">
    <header class="admin-header">
      <div><p class="admin-eyebrow">GOPULSE ADMIN</p><h1>可观测中心</h1></div>
      <RouterLink class="button button--secondary" to="/posts">返回社交</RouterLink>
    </header>
    <nav class="admin-nav" aria-label="可观测导航">
      <RouterLink to="/admin/observability" exact-active-class="router-link-active">总览</RouterLink>
      <RouterLink to="/admin/observability/metrics">Metrics</RouterLink>
      <RouterLink to="/admin/observability/logs">Logs</RouterLink>
      <RouterLink to="/admin/observability/events">Events</RouterLink>
      <RouterLink to="/admin/observability/exporters">Exporter</RouterLink>
    </nav>
    <main class="admin-content"><RouterView /></main>
  </div>
</template>
