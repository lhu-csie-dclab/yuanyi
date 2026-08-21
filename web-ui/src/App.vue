<script setup>
import { ref, computed } from 'vue'
import { useNodeInfo } from './composables/useNodeInfo.js'
import { useI18n } from './composables/useI18n.js'
import Toast from './components/Toast.vue'

const nodeInfo = useNodeInfo()
const { t, langLabel, toggleLang } = useI18n()
const mobileMenuOpen = ref(false)

const avatarText = computed(() =>
  nodeInfo.localNodeId ? nodeInfo.localNodeId.substring(0, 2).toUpperCase() : 'MC'
)
const shortId = computed(() =>
  nodeInfo.localNodeId ? nodeInfo.localNodeId.substring(0, 14) + '...' : t('status_connecting')
)
</script>

<template>
  <div class="flex min-h-screen overflow-x-hidden">
    <!-- ─── Backdrop ─── -->
    <div
      v-if="mobileMenuOpen"
      class="fixed inset-0 z-40 bg-black/50 lg:hidden"
      @click="mobileMenuOpen = false"
    />

    <!-- ═══════════════════════════════════════════
         SIDEBAR — dark slate
    ════════════════════════════════════════════ -->
    <aside
      :class="[
        'fixed inset-y-0 left-0 z-50 flex w-60 flex-col bg-[#1e293b] transition-transform duration-300 ease-in-out lg:translate-x-0',
        mobileMenuOpen ? 'translate-x-0' : '-translate-x-full'
      ]"
    >
      <!-- Logo -->
      <div class="flex h-16 items-center gap-3 border-b border-white/[0.07] px-5">
        <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-blue-500 to-indigo-600 text-sm shadow-lg">
          🌙
        </div>
        <div>
          <div class="text-sm font-bold text-white leading-tight">Mooncake</div>
          <div class="text-[0.68rem] text-slate-500">P2P Node Gateway</div>
        </div>
        <button class="ml-auto text-slate-500 hover:text-white lg:hidden" @click="mobileMenuOpen = false">✕</button>
      </div>

      <!-- Nav -->
      <nav class="flex-1 overflow-y-auto py-4 px-3 space-y-0.5">
        <div class="nav-section">{{ t('nav_monitor') }}</div>
        <router-link to="/" class="nav-link" @click="mobileMenuOpen = false">
          <span class="text-base">📊</span> {{ t('nav_topology') }}
        </router-link>
        <router-link to="/top" class="nav-link" @click="mobileMenuOpen = false">
          <span class="text-base">🏆</span> {{ t('nav_top') }}
        </router-link>
        <router-link to="/chat" class="nav-link" @click="mobileMenuOpen = false">
          <span class="text-base">💬</span> {{ t('nav_chat') }}
        </router-link>
        <router-link to="/logs" class="nav-link" @click="mobileMenuOpen = false">
          <span class="text-base">📜</span> {{ t('nav_logs') }}
        </router-link>

        <div class="nav-section">{{ t('nav_manage') }}</div>
        <router-link to="/settings" class="nav-link" @click="mobileMenuOpen = false">
          <span class="text-base">⚙️</span> {{ t('nav_settings') }}
        </router-link>

        <div class="nav-section">{{ t('nav_dev') }}</div>
        <router-link to="/api" class="nav-link" @click="mobileMenuOpen = false">
          <span class="text-base">🔗</span> {{ t('nav_api') }}
        </router-link>

        <template v-if="nodeInfo.hubModeEnabled">
          <div class="nav-section">{{ t('nav_cluster') }}</div>
          <router-link to="/hub" class="nav-link" @click="mobileMenuOpen = false">
            <span class="text-base">🛰️</span> {{ t('nav_hub_topology') }}
          </router-link>
          <router-link to="/hub/history" class="nav-link" @click="mobileMenuOpen = false">
            <span class="text-base">📋</span> {{ t('nav_hub_history') }}
          </router-link>
          <router-link to="/hub/leaderboard" class="nav-link" @click="mobileMenuOpen = false">
            <span class="text-base">🏆</span> {{ t('nav_hub_board') }}
          </router-link>
        </template>
      </nav>

      <!-- Footer -->
      <div class="border-t border-white/[0.07] px-4 py-3 space-y-2">
        <!-- Lang toggle -->
        <button
          class="w-full flex items-center justify-between rounded-xl px-3 py-1.5 text-[0.72rem] font-semibold text-slate-400 hover:bg-white/10 hover:text-white transition-colors"
          @click="toggleLang"
        >
          <span class="flex items-center gap-1.5"><span>🌐</span> Language</span>
          <span class="rounded-md bg-white/10 px-2 py-0.5 font-bold text-white text-[0.65rem]">{{ langLabel }}</span>
        </button>
        <!-- Node info -->
        <div class="flex items-center gap-3">
          <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white">
            {{ avatarText }}
          </div>
          <div class="min-w-0 flex-1">
            <div class="truncate font-mono text-[0.7rem] text-slate-300">{{ shortId }}</div>
            <div class="flex items-center gap-1.5 mt-0.5">
              <span class="h-1.5 w-1.5 rounded-full" :class="nodeInfo.loaded ? 'bg-emerald-400' : 'bg-amber-400 animate-pulse'" />
              <span class="text-[0.68rem] text-slate-500">{{ nodeInfo.loaded ? t('status_online') : t('status_connecting') }}</span>
            </div>
          </div>
        </div>
      </div>
    </aside>

    <!-- ═══════════════════════════════════════════
         MAIN — light content area
    ════════════════════════════════════════════ -->
    <div class="flex flex-1 flex-col min-w-0 lg:pl-60">
      <!-- Mobile topbar -->
      <header class="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-slate-200 bg-white/95 px-4 backdrop-blur-md lg:hidden">
        <button
          class="flex h-9 w-9 items-center justify-center rounded-lg border border-slate-200 text-slate-600 hover:bg-slate-50"
          @click="mobileMenuOpen = true"
        >
          <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
        <span class="font-bold text-slate-900 flex items-center gap-2"><span>🌙</span> Mooncake</span>
        <button class="text-xs text-slate-500 hover:text-slate-800 border border-slate-200 rounded-lg px-2 py-1" @click="toggleLang">{{ langLabel }}</button>
      </header>

      <!-- Page content -->
      <main class="flex-1 min-w-0 max-w-full overflow-x-hidden bg-[#f0f4f9]">
        <router-view />
      </main>
    </div>

    <Toast />
  </div>
</template>
