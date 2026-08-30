<script setup>
import { ref, computed } from 'vue'
import { getClusterStats, getPeers, getLocalStats, fmtNum, fmtUptime, fmtMB, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useNodeInfo } from '../../composables/useNodeInfo.js'
import { useI18n } from '../../composables/useI18n.js'
import PageHeader from '../../components/PageHeader.vue'
import StatCard from '../../components/StatCard.vue'
import Pagination from '../../components/Pagination.vue'
import PeerCard from '../../components/PeerCard.vue'
import PeerListRow from '../../components/PeerListRow.vue'

const nodeInfo = useNodeInfo()
const { t } = useI18n()
const stats = ref({ total_nodes: 0, total_active_requests: 0, total_gen_speed: 0, avg_ttft: 0, total_tokens: 0, in_tokens: 0, out_tokens: 0, total_requests: 0, total_power_draw: 0, total_power_limit: 0 })
const peers = ref([])
const local = ref({ total_tokens: 0, in_tokens: 0, out_tokens: 0, total_requests: 0, success_count: 0, uptime_seconds: 0, cpu_percent: 0, mem_rss_mb: 0 })

// Card grid vs. dense table -- persisted per-browser so it doesn't reset every visit.
const VIEW_MODE_KEY = 'topology_view_mode'
const viewMode = ref(localStorage.getItem(VIEW_MODE_KEY) || 'grid')
function setViewMode(mode) {
  viewMode.value = mode
  localStorage.setItem(VIEW_MODE_KEY, mode)
}

// Sorted by live power draw (watts), highest first. A client-side sort is mandatory here at
// all: /api/peers is built from a Go map, whose iteration order the language deliberately
// randomizes, so with no sort the list visibly reshuffled on every 2s poll.
//
// Power draw is a live value, so unlike a fixed hardware property the order does legitimately
// change as load moves between nodes -- that is the point, it surfaces which nodes are
// actually working. On a homogeneous cluster (identical cards) it is also the only telemetry
// that differentiates peers at all; sorting by VRAM there produced one big tie, i.e. no
// meaningful order. Ties still fall back to peer ID so equal-draw (e.g. all-idle) nodes hold a
// stable order instead of jittering every poll.
const sortedPeers = computed(() => {
  return [...peers.value]
    .map(p => ({ ...p, _gpu: parseGpuInfo(p) }))
    .sort((a, b) => {
      const diff = (b._gpu?.power_draw || 0) - (a._gpu?.power_draw || 0)
      if (diff !== 0) return diff
      return (a.peer_id || a.node_id || '').localeCompare(b.peer_id || b.node_id || '')
    })
})

// Pagination (25 items per page)
const PAGE_SIZE = 25
const currentPage = ref(1)

const totalPages = computed(() => Math.max(1, Math.ceil(sortedPeers.value.length / PAGE_SIZE)))

const paginatedPeers = computed(() => {
  const start = (currentPage.value - 1) * PAGE_SIZE
  return sortedPeers.value.slice(start, start + PAGE_SIZE)
})

// Top pill-badge counts by category, shown above the peer grid/table.
const gpuCount = computed(() => peers.value.filter(p => !!parseGpuInfo(p).summary).length)
const relayCount = computed(() => peers.value.length - gpuCount.value)
const busyCount = computed(() => peers.value.filter(p => (parseGpuInfo(p).status || 'idle') !== 'idle').length)

async function refresh() {
  try {
    const [s, p] = await Promise.all([getClusterStats(), getPeers()])
    stats.value = s
    peers.value = p || []
  } catch {}
  try {
    local.value = await getLocalStats()
  } catch {}
}
usePolling(refresh, 2000)
</script>

<template>
  <PageHeader :title="t('page_topology')" :badge="t('badge_nodes', stats.total_nodes || 0)" />

  <div class="p-5 space-y-5 max-w-full min-w-0">

    <!-- ① Cluster Quick Summary Row -->
    <div class="rounded-2xl bg-surface-card border border-border shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3 border-b border-white/5 bg-white/[0.03]">
        <div class="flex items-center gap-2">
          <span class="text-base">🌐</span>
          <span class="text-xs font-bold uppercase tracking-widest text-ink-muted">{{ t('section_cluster') || 'Cluster Summary' }}</span>
        </div>
        <span class="pill pill-green"><span class="h-1.5 w-1.5 rounded-full bg-emerald-400 inline-block mr-1"/>{{ t('status_online') }}</span>
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-9 divide-x divide-white/5">
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_nodes') }}</span>
          <span class="text-lg font-bold text-brand-light tabular-nums">{{ stats.total_nodes || 0 }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_requests') }}</span>
          <span class="text-lg font-bold text-violet-400 tabular-nums">{{ stats.total_active_requests || 0 }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_throughput') }}</span>
          <span class="text-lg font-bold text-emerald-400 tabular-nums">{{ (stats.total_gen_speed || 0).toFixed(1) }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_ttft') }}</span>
          <span class="text-lg font-bold text-amber-400 tabular-nums">{{ (stats.avg_ttft || 0).toFixed(2) }}s</span>
        </div>
        <div class="px-4 py-3 flex flex-col col-span-2 sm:col-span-2 lg:col-span-2">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_total_tokens') }}</span>
          <span class="text-lg font-bold text-ink tabular-nums">{{ fmtNum(stats.total_tokens) }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_input') }}</span>
          <span class="text-lg font-bold text-ink-muted tabular-nums">{{ fmtNum(stats.in_tokens) }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_output') }}</span>
          <span class="text-lg font-bold text-ink-muted tabular-nums">{{ fmtNum(stats.out_tokens) }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col" :title="`${fmtNum(Math.round(stats.total_power_limit || 0))} W rated capacity`">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('stat_power') }}</span>
          <span class="text-lg font-bold text-rose-400 tabular-nums">{{ fmtNum(Math.round(stats.total_power_draw || 0)) }} W</span>
        </div>
      </div>
    </div>

    <!-- ② Local Contribution -->
    <div class="rounded-2xl border border-brand/25 bg-gradient-to-r from-brand/10 to-cyan/5 p-5">
      <div class="flex items-center justify-between mb-4">
        <span class="text-xs font-bold uppercase tracking-widest text-brand-light">{{ t('section_local') }}</span>
        <span class="pill pill-blue">{{ t('status_online') }} {{ fmtUptime(local.uptime_seconds) }}</span>
      </div>
      <div class="grid grid-cols-4 gap-4 sm:grid-cols-7 divide-x divide-white/10">
        <StatCard bare :label="t('stat_total_tokens')" :value="fmtNum(local.total_tokens)"    accent />
        <StatCard bare :label="t('stat_input')"        :value="fmtNum(local.in_tokens)"       class="pl-4" />
        <StatCard bare :label="t('stat_output')"       :value="fmtNum(local.out_tokens)"      class="pl-4" />
        <StatCard bare :label="t('stat_requests')"     :value="fmtNum(local.total_requests)"  class="pl-4" />
        <StatCard bare label="OK"                      :value="fmtNum(local.success_count)"   class="pl-4" />
        <StatCard bare :label="t('stat_cpu')"          :value="`${(local.cpu_percent || 0).toFixed(1)}%`" class="pl-4" />
        <StatCard bare :label="t('stat_memory')"       :value="fmtMB(local.mem_rss_mb)"       class="pl-4" />
      </div>
    </div>

    <!-- ③ P2P Node List (Paginated by 25) -->
    <div class="rounded-2xl border border-border bg-surface-card shadow-sm overflow-hidden">
      <div class="flex flex-wrap items-center justify-between gap-2 px-5 py-3.5 border-b border-white/5 bg-white/[0.03]">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
          <span class="font-semibold text-sm text-ink">{{ t('section_peers') }}</span>
        </div>
        <div class="flex items-center gap-2 min-w-0 max-w-full overflow-x-auto flex-nowrap sm:flex-wrap sm:overflow-visible">
          <span class="pill pill-cyan shrink-0">{{ t('gpu_nodes_count', gpuCount) }}</span>
          <span v-if="relayCount" class="pill pill-blue shrink-0">{{ t('relay_nodes_count', relayCount) }}</span>
          <span v-if="busyCount" class="pill pill-amber shrink-0">{{ t('busy_now_count', busyCount) }}</span>
          <span class="text-xs text-ink-faint shrink-0">{{ t('peer_count', peers.length) }}</span>
          <!-- Grid/list toggle -->
          <div class="flex items-center rounded-lg border border-border bg-white/[0.03] p-0.5 shrink-0">
            <button
              class="rounded-md px-2 py-1 text-xs font-semibold transition-colors"
              :class="viewMode === 'grid' ? 'bg-brand/20 text-brand-light' : 'text-ink-faint hover:text-ink'"
              :title="t('view_grid')"
              @click="setViewMode('grid')"
            >▦</button>
            <button
              class="rounded-md px-2 py-1 text-xs font-semibold transition-colors"
              :class="viewMode === 'list' ? 'bg-brand/20 text-brand-light' : 'text-ink-faint hover:text-ink'"
              :title="t('view_list')"
              @click="setViewMode('list')"
            >☰</button>
          </div>
        </div>
      </div>

      <!-- Empty state (shared by both view modes) -->
      <div v-if="!paginatedPeers.length" class="py-12 text-center text-ink-faint text-sm">{{ t('loading_nodes') }}</div>

      <!-- Card grid. min-w-0 + max-w-full + box-border on the grid itself so it can never be
           the thing forcing horizontal overflow; each PeerCard repeats the same guards. -->
      <div v-else-if="viewMode === 'grid'" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 p-4 w-full max-w-full min-w-0 box-border">
        <PeerCard
          v-for="(p, i) in paginatedPeers"
          :key="p.peer_id || p.node_id || i"
          :peer="p"
          :is-local="(p.peer_id || p.node_id) === nodeInfo.localNodeId"
        />
      </div>

      <!-- List: two-tier rows (identity row + wrapping telemetry sub-row), not a <table> --
           a table forced the IP/multiaddr and telemetry columns to blow the row width out
           (see PeerListRow.vue for the full explanation). -->
      <div v-else class="w-full max-w-full overflow-x-auto box-border">
        <PeerListRow
          v-for="(p, i) in paginatedPeers"
          :key="p.peer_id || p.node_id || i"
          :peer="p"
          :is-local="(p.peer_id || p.node_id) === nodeInfo.localNodeId"
          :rank="(currentPage - 1) * PAGE_SIZE + i + 1"
        />
      </div>

      <!-- Pagination when > 25 -->
      <Pagination
        v-model:currentPage="currentPage"
        :totalPages="totalPages"
        :totalItems="peers.length"
        :pageSize="PAGE_SIZE"
      />
    </div>

  </div>
</template>
