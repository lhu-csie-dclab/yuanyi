<script setup>
import { ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { useNodeInfo } from './composables/useNodeInfo.js'
import Toast from './components/Toast.vue'

const nodeInfo = useNodeInfo()
const route = useRoute()
const mobileMenuOpen = ref(false)

const avatarText = computed(() =>
  nodeInfo.localNodeId ? nodeInfo.localNodeId.substring(0, 2).toUpperCase() : 'MC'
)
const shortNodeId = computed(() =>
  nodeInfo.localNodeId ? nodeInfo.localNodeId.substring(0, 16) + '...' : 'Node: connecting...'
)

function closeMobileMenu() {
  mobileMenuOpen.value = false
}
</script>

<template>
  <div class="flex min-h-screen bg-[#f8fafc] text-slate-800 antialiased">
    <!-- MOBILE BACKDROP OVERLAY -->
    <div
      v-if="mobileMenuOpen"
      class="fixed inset-0 z-40 bg-slate-900/40 backdrop-blur-xs transition-opacity lg:hidden"
      @click="closeMobileMenu"
    />

    <!-- SIDEBAR (Responsive Off-canvas on Mobile, Pinned on Desktop) -->
    <aside
      :class="[
        'fixed inset-y-0 left-0 z-50 flex w-64 max-w-[80vw] flex-col border-r border-slate-200 bg-white transition-transform duration-300 ease-in-out lg:translate-x-0 shadow-xs',
        mobileMenuOpen ? 'translate-x-0 shadow-2xl' : '-translate-x-full'
      ]"
    >
      <!-- Logo Brand -->
      <div class="flex items-center justify-between border-b border-slate-100 px-5 py-5">
        <div class="flex items-center gap-3">
          <div class="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-blue-600 to-indigo-600 text-lg font-bold text-white shadow-xs">
            🌙
          </div>
          <div>
            <div class="text-base font-bold leading-none text-slate-900">Mooncake</div>
            <div class="mt-1 text-[0.7rem] font-medium text-slate-400">Node &amp; Gateway</div>
          </div>
        </div>
        <!-- Close button on mobile -->
        <button
          class="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700 lg:hidden"
          aria-label="Close Sidebar"
          @click="closeMobileMenu"
        >
          <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>

      <!-- Navigation -->
      <nav class="flex-1 overflow-y-auto space-y-1 p-3">
        <div class="px-3 pb-1.5 pt-3 text-[0.68rem] font-bold uppercase tracking-wider text-slate-400">
          Monitoring
        </div>
        <router-link to="/" class="nav-link" @click="closeMobileMenu"><span class="w-5 text-center">📊</span> Topology &amp; Rank</router-link>
        <router-link to="/logs" class="nav-link" @click="closeMobileMenu"><span class="w-5 text-center">📜</span> Real-time Logs</router-link>

        <div class="px-3 pb-1.5 pt-4 text-[0.68rem] font-bold uppercase tracking-wider text-slate-400">
          Management
        </div>
        <router-link to="/settings" class="nav-link" @click="closeMobileMenu"><span class="w-5 text-center">⚙️</span> Config &amp; Backups</router-link>

        <div class="px-3 pb-1.5 pt-4 text-[0.68rem] font-bold uppercase tracking-wider text-slate-400">
          Developer
        </div>
        <router-link to="/api" class="nav-link" @click="closeMobileMenu"><span class="w-5 text-center">🔗</span> OpenAI /v1 API</router-link>

        <template v-if="nodeInfo.hubModeEnabled">
          <div class="px-3 pb-1.5 pt-4 text-[0.68rem] font-bold uppercase tracking-wider text-slate-400">
            Cluster (Hub Mode)
          </div>
          <router-link to="/hub" class="nav-link" @click="closeMobileMenu"><span class="w-5 text-center">🛰️</span> Active Topology</router-link>
          <router-link to="/hub/history" class="nav-link" @click="closeMobileMenu"><span class="w-5 text-center">📋</span> Global History</router-link>
          <router-link to="/hub/leaderboard" class="nav-link" @click="closeMobileMenu"><span class="w-5 text-center">🏆</span> Leaderboard</router-link>
        </template>
      </nav>

      <!-- Bottom User/Node Info -->
      <div class="flex items-center gap-3 border-t border-slate-100 bg-slate-50/60 px-4 py-3.5 sm:px-5">
        <div class="flex h-8.5 w-8.5 shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white shadow-xs">
          {{ avatarText }}
        </div>
        <div class="min-w-0 flex-1">
          <div class="truncate font-mono text-xs font-semibold text-slate-800">{{ shortNodeId }}</div>
          <div class="flex items-center gap-1.5 text-[0.7rem] text-slate-500">
            <span class="inline-block h-1.5 w-1.5 rounded-full" :class="nodeInfo.loaded ? 'bg-emerald-500' : 'bg-amber-500 animate-pulse'" />
            {{ nodeInfo.loaded ? 'Online' : 'Connecting...' }}
          </div>
        </div>
      </div>
    </aside>

    <!-- MAIN WRAPPER (Responsive Margin & Overflow Control) -->
    <div class="flex flex-1 flex-col min-w-0 max-w-full lg:pl-64">
      <!-- MOBILE TOP NAV HEADER -->
      <header class="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-slate-200 bg-white/95 px-4 backdrop-blur-md lg:hidden">
        <div class="flex items-center gap-3">
          <button
            class="flex h-9 w-9 items-center justify-center rounded-lg border border-slate-200 bg-slate-50 text-slate-700 hover:bg-slate-100"
            aria-label="Open Navigation Menu"
            @click="mobileMenuOpen = true"
          >
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>
          <div class="flex items-center gap-2">
            <span class="text-lg">🌙</span>
            <span class="font-bold tracking-tight text-slate-900">Mooncake</span>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span class="flex items-center gap-1.5 rounded-full bg-slate-100 px-2.5 py-1 text-[0.7rem] font-mono text-slate-600 border border-slate-200">
            <span class="h-1.5 w-1.5 rounded-full" :class="nodeInfo.loaded ? 'bg-emerald-500' : 'bg-amber-500 animate-pulse'" />
            {{ nodeInfo.loaded ? 'Online' : 'Connecting' }}
          </span>
        </div>
      </header>

      <!-- VIEW CONTENT CONTAINER -->
      <main class="flex-1 min-w-0 max-w-full overflow-x-hidden">
        <router-view />
      </main>
    </div>

    <Toast />
  </div>
</template>

