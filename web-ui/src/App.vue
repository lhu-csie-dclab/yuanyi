<script setup>
import { ref, computed } from 'vue'
import { useRoute } from 'vue-router'
import { useNodeInfo } from './composables/useNodeInfo.js'
import Toast from './components/Toast.vue'

const nodeInfo = useNodeInfo()
const route = useRoute()
const mobileMenuOpen = ref(false)
const searchQuery = ref('')

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
  <div class="min-h-screen bg-[#f4f6fb] text-slate-800 antialiased flex flex-col">
    <!-- TOP SIGNATURE OCEAN BLUE NAV BAR -->
    <header class="sticky top-0 z-50 bg-[#1c5b88] text-white shadow-md">
      <div class="flex h-14 items-center justify-between px-3 sm:px-6">
        <!-- Left: Hamburger + Cluster Selector -->
        <div class="flex items-center gap-3">
          <button
            class="flex h-9 w-9 items-center justify-center rounded-lg text-white/90 hover:bg-white/15 transition-colors"
            aria-label="Toggle Menu"
            @click="mobileMenuOpen = !mobileMenuOpen"
          >
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
            </svg>
          </button>
          
          <div class="flex items-center gap-2 cursor-pointer rounded-lg px-2 py-1 hover:bg-white/10 transition-colors">
            <span class="text-sm font-semibold tracking-wide flex items-center gap-1.5">
              <span>🌙</span> Mooncake P2P Swarm
            </span>
            <svg class="h-4 w-4 opacity-70" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </div>

        <!-- Center: Distinctive Minimalist Brand Logo -->
        <div class="hidden md:flex items-center justify-center">
          <div class="flex h-8 w-14 items-center justify-center rounded-md bg-white/15 backdrop-blur-xs font-mono font-bold tracking-widest text-xs">
            P2P
          </div>
        </div>

        <!-- Right: Search Bar + App Grid + Help + Avatar -->
        <div class="flex items-center gap-2 sm:gap-3">
          <!-- Search Bar -->
          <div class="relative hidden sm:block">
            <input
              v-model="searchQuery"
              type="text"
              placeholder="Search..."
              class="h-8 w-44 md:w-56 rounded-md bg-[#154668] border border-white/20 px-3 pr-8 text-xs text-white placeholder-white/60 focus:outline-none focus:ring-1 focus:ring-white/40"
            />
            <svg class="absolute right-2.5 top-2 h-4 w-4 text-white/60" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
          </div>

          <!-- App Grid Launcher Icon -->
          <button class="flex h-8 w-8 items-center justify-center rounded-md text-white/80 hover:bg-white/15 transition-colors" title="Applications">
            <svg class="h-4 w-4" fill="currentColor" viewBox="0 0 24 24">
              <circle cx="5" cy="5" r="2" /><circle cx="12" cy="5" r="2" /><circle cx="19" cy="5" r="2" />
              <circle cx="5" cy="12" r="2" /><circle cx="12" cy="12" r="2" /><circle cx="19" cy="12" r="2" />
              <circle cx="5" cy="19" r="2" /><circle cx="12" cy="19" r="2" /><circle cx="19" cy="19" r="2" />
            </svg>
          </button>

          <!-- Help Icon -->
          <button class="flex h-8 w-8 items-center justify-center rounded-full text-white/80 hover:bg-white/15 transition-colors font-bold text-xs border border-white/30" title="Help">
            ?
          </button>

          <!-- Avatar Pill -->
          <div class="flex items-center gap-2 rounded-full bg-[#f97316] text-white h-8 w-8 justify-center font-bold text-xs shadow-xs" title="Node User">
            {{ avatarText }}
          </div>
        </div>
      </div>
    </header>

    <!-- SUB-NAVIGATION BAR (Horizontal Tabs & Breadcrumbs) -->
    <div class="sticky top-14 z-40 bg-white border-b border-slate-200 shadow-xs">
      <div class="flex items-center justify-between px-3 sm:px-6 overflow-x-auto">
        <!-- Navigation Tabs -->
        <nav class="flex items-center space-x-1">
          <router-link to="/" class="nav-tab">
            <span>📊</span> Topology &amp; Rank
          </router-link>
          <router-link to="/logs" class="nav-tab">
            <span>📜</span> Real-time Logs
          </router-link>
          <router-link to="/settings" class="nav-tab">
            <span>⚙️</span> Config &amp; Backups
          </router-link>
          <router-link to="/api" class="nav-tab">
            <span>🔗</span> OpenAI /v1 API
          </router-link>

          <template v-if="nodeInfo.hubModeEnabled">
            <div class="h-5 w-px bg-slate-200 mx-2" />
            <router-link to="/hub" class="nav-tab">
              <span>🛰️</span> Active Topology
            </router-link>
            <router-link to="/hub/history" class="nav-tab">
              <span>📋</span> Global History
            </router-link>
            <router-link to="/hub/leaderboard" class="nav-tab">
              <span>🏆</span> Leaderboard
            </router-link>
          </template>
        </nav>

        <!-- Right Quick Status Capsule -->
        <div class="hidden md:flex items-center gap-3 py-2">
          <span class="inline-flex items-center gap-1.5 rounded-full bg-slate-100 px-3 py-1 text-xs font-mono text-slate-600 border border-slate-200">
            <span class="h-2 w-2 rounded-full" :class="nodeInfo.loaded ? 'bg-emerald-500' : 'bg-amber-500 animate-pulse'" />
            {{ shortNodeId }}
          </span>
        </div>
      </div>
    </div>

    <!-- MOBILE SLIDE-OUT DRAWER -->
    <div
      v-if="mobileMenuOpen"
      class="fixed inset-0 z-50 bg-black/50 backdrop-blur-xs transition-opacity lg:hidden"
      @click="closeMobileMenu"
    />
    <aside
      :class="[
        'fixed inset-y-0 left-0 z-50 flex w-72 max-w-[80vw] flex-col bg-white border-r border-slate-200 shadow-2xl transition-transform duration-300 ease-in-out lg:hidden',
        mobileMenuOpen ? 'translate-x-0' : '-translate-x-full'
      ]"
    >
      <div class="flex items-center justify-between bg-[#1c5b88] text-white px-5 py-4">
        <div class="font-bold text-sm flex items-center gap-2">
          <span>🌙</span> Mooncake Menu
        </div>
        <button class="p-1 text-white/80 hover:text-white" @click="closeMobileMenu">✕</button>
      </div>

      <nav class="flex-1 overflow-y-auto p-4 space-y-1">
        <div class="text-[0.7rem] font-bold uppercase text-slate-400 px-3 pt-2 pb-1">Client Modules</div>
        <router-link to="/" class="nav-link" @click="closeMobileMenu"><span>📊</span> Topology &amp; Rank</router-link>
        <router-link to="/logs" class="nav-link" @click="closeMobileMenu"><span>📜</span> Real-time Logs</router-link>
        <router-link to="/settings" class="nav-link" @click="closeMobileMenu"><span>⚙️</span> Config &amp; Backups</router-link>
        <router-link to="/api" class="nav-link" @click="closeMobileMenu"><span>🔗</span> OpenAI /v1 API</router-link>

        <template v-if="nodeInfo.hubModeEnabled">
          <div class="text-[0.7rem] font-bold uppercase text-slate-400 px-3 pt-4 pb-1">Hub (Cluster Mode)</div>
          <router-link to="/hub" class="nav-link" @click="closeMobileMenu"><span>🛰️</span> Active Topology</router-link>
          <router-link to="/hub/history" class="nav-link" @click="closeMobileMenu"><span>📋</span> Global History</router-link>
          <router-link to="/hub/leaderboard" class="nav-link" @click="closeMobileMenu"><span>🏆</span> Leaderboard</router-link>
        </template>
      </nav>
    </aside>

    <!-- MAIN VIEW CONTAINER -->
    <main class="flex-1 min-w-0 max-w-full overflow-x-hidden">
      <router-view />
    </main>

    <Toast />
  </div>
</template>

