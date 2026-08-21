import { createRouter, createWebHashHistory } from 'vue-router'

// Hash-based routing on purpose: the Go server just serves one static SPA
// bundle at "/" with no fallback routing logic needed, since the route
// (everything after "#") never leaves the browser. /api/* and /hub/api/*
// stay real server paths, untouched by this router.
const routes = [
  { path: '/', name: 'client-topology', component: () => import('./views/client/TopologyView.vue') },
  { path: '/logs', name: 'client-logs', component: () => import('./views/client/LogsView.vue') },
  { path: '/settings', name: 'client-settings', component: () => import('./views/client/SettingsView.vue') },
  { path: '/api', name: 'client-api', component: () => import('./views/client/ApiView.vue') },
  { path: '/hub', name: 'hub-topology', component: () => import('./views/hub/HubTopologyView.vue') },
  { path: '/hub/history', name: 'hub-history', component: () => import('./views/hub/HubHistoryView.vue') },
  { path: '/hub/leaderboard', name: 'hub-leaderboard', component: () => import('./views/hub/HubLeaderboardView.vue') },
]

export default createRouter({
  history: createWebHashHistory(),
  routes,
})
