<script setup>
import { computed } from 'vue'
import { useNodeInfo } from './composables/useNodeInfo.js'
import Toast from './components/Toast.vue'

const nodeInfo = useNodeInfo()

const avatarText = computed(() =>
  nodeInfo.localNodeId ? nodeInfo.localNodeId.substring(0, 2).toUpperCase() : 'N'
)
const shortNodeId = computed(() =>
  nodeInfo.localNodeId ? nodeInfo.localNodeId.substring(0, 16) + '...' : 'Node: connecting...'
)
</script>

<template>
  <div class="flex min-h-screen bg-surface text-ink">
    <!-- SIDEBAR -->
    <aside class="fixed inset-y-0 left-0 z-40 flex w-64 flex-col overflow-y-auto border-r border-border bg-surface-raised">
      <div class="flex items-center gap-3 border-b border-border px-5 py-6">
        <div class="flex h-9 w-9 items-center justify-center rounded-[10px] bg-gradient-to-br from-brand to-cyan text-lg font-bold text-white">
          🌙
        </div>
        <div>
          <div class="text-base font-bold leading-none">Mooncake</div>
          <div class="mt-1 text-[0.7rem] text-ink-muted">Node & Gateway</div>
        </div>
      </div>

      <nav class="flex-1 space-y-0.5 p-3">
        <div class="px-3 pb-2 pt-4 text-[0.65rem] font-semibold uppercase tracking-wider text-ink-muted">
          Monitoring
        </div>
        <router-link to="/" class="nav-link"><span class="w-5 text-center">📊</span> Topology &amp; Rank</router-link>
        <router-link to="/logs" class="nav-link"><span class="w-5 text-center">📜</span> Real-time Logs</router-link>

        <div class="px-3 pb-2 pt-4 text-[0.65rem] font-semibold uppercase tracking-wider text-ink-muted">
          Management
        </div>
        <router-link to="/settings" class="nav-link"><span class="w-5 text-center">⚙️</span> Config &amp; Backups</router-link>

        <div class="px-3 pb-2 pt-4 text-[0.65rem] font-semibold uppercase tracking-wider text-ink-muted">
          Developer
        </div>
        <router-link to="/api" class="nav-link"><span class="w-5 text-center">🔗</span> OpenAI /v1 API</router-link>

        <template v-if="nodeInfo.hubModeEnabled">
          <div class="px-3 pb-2 pt-4 text-[0.65rem] font-semibold uppercase tracking-wider text-ink-muted">
            Cluster (Hub Mode)
          </div>
          <router-link to="/hub" class="nav-link"><span class="w-5 text-center">🛰️</span> Active Topology</router-link>
          <router-link to="/hub/history" class="nav-link"><span class="w-5 text-center">📋</span> Global History</router-link>
          <router-link to="/hub/leaderboard" class="nav-link"><span class="w-5 text-center">🏆</span> Leaderboard</router-link>
        </template>
      </nav>

      <div class="flex items-center gap-3 border-t border-border px-5 py-4">
        <div class="flex h-8.5 w-8.5 items-center justify-center rounded-full bg-gradient-to-br from-good to-cyan text-xs font-bold text-white">
          {{ avatarText }}
        </div>
        <div class="text-sm">
          <div class="font-mono text-xs">{{ shortNodeId }}</div>
          <div class="text-[0.7rem] text-ink-muted">{{ nodeInfo.loaded ? 'Online' : 'Connecting...' }}</div>
        </div>
      </div>
    </aside>

    <!-- MAIN -->
    <div class="ml-64 flex-1">
      <router-view />
    </div>

    <Toast />
  </div>
</template>
