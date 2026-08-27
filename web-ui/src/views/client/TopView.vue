<script setup>
import { ref, computed } from 'vue'
import { getClusterStats, getPeers, fmtNum, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useNodeInfo } from '../../composables/useNodeInfo.js'
import { useI18n } from '../../composables/useI18n.js'
import PageHeader from '../../components/PageHeader.vue'
import Pagination from '../../components/Pagination.vue'
import PeerCard from '../../components/PeerCard.vue'
import PeerListRow from '../../components/PeerListRow.vue'

const MEDALS = ['🥇', '🥈', '🥉']

const nodeInfo = useNodeInfo()
const { t } = useI18n()
const stats = ref({ total_nodes: 0, total_active_requests: 0, total_gen_speed: 0, avg_ttft: 0 })
const peers = ref([])

// Card grid vs. dense table -- persisted per-browser so it doesn't reset every visit.
const VIEW_MODE_KEY = 'top_view_mode'
const viewMode = ref(localStorage.getItem(VIEW_MODE_KEY) || 'grid')
function setViewMode(mode) {
  viewMode.value = mode
  localStorage.setItem(VIEW_MODE_KEY, mode)
}

// Pagination (25 items per page)
const PAGE_SIZE = 25
const currentPage = ref(1)

// All peers sorted by throughput (gen_speed) descending
const sortedPeers = computed(() => {
  return [...peers.value]
    .map(p => ({ ...p, _gpu: parseGpuInfo(p) }))
    .sort((a, b) => {
      const speedA = a._gpu?.gen_speed || 0
      const speedB = b._gpu?.gen_speed || 0
      return speedB - speedA
    })
})

const totalPages = computed(() => Math.max(1, Math.ceil(sortedPeers.value.length / PAGE_SIZE)))

const paginatedPeers = computed(() => {
  const start = (currentPage.value - 1) * PAGE_SIZE
  return sortedPeers.value.slice(start, start + PAGE_SIZE)
})

async function refresh() {
  try {
    const [s, p] = await Promise.all([getClusterStats(), getPeers()])
    stats.value = s
    peers.value = p || []
  } catch {}
}
usePolling(refresh, 2000)
</script>

<template>
  <PageHeader :title="t('page_top')" :badge="t('badge_nodes', sortedPeers.length)" />

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
      <div class="grid grid-cols-2 sm:grid-cols-4 divide-x divide-white/5">
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
      </div>
    </div>

    <!-- ② TOP Ranking (Paginated by 25) -->
    <div class="rounded-2xl border border-border bg-surface-card shadow-sm overflow-hidden">
      <div class="flex flex-wrap items-center justify-between gap-2 px-5 py-3.5 border-b border-white/5 bg-white/[0.03]">
        <div class="flex items-center gap-2">
          <span class="text-base">🏆</span>
          <h2 class="text-sm font-semibold text-ink">{{ t('top_nodes') }}</h2>
        </div>
        <div class="flex items-center gap-2 min-w-0 max-w-full overflow-x-auto flex-nowrap sm:flex-wrap sm:overflow-visible">
          <span class="text-xs text-ink-faint shrink-0">{{ t('by_throughput') }}</span>
          <span class="text-xs text-ink-faint shrink-0">·</span>
          <span class="text-xs text-ink-faint shrink-0">{{ t('peer_count', sortedPeers.length) }}</span>
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

      <!-- Card grid -->
      <div v-else-if="viewMode === 'grid'" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3 p-4 w-full max-w-full min-w-0 box-border">
        <PeerCard
          v-for="(p, i) in paginatedPeers"
          :key="p.peer_id || p.node_id || i"
          :peer="p"
          :is-local="(p.peer_id || p.node_id) === nodeInfo.localNodeId"
          :medal="MEDALS[(currentPage - 1) * PAGE_SIZE + i] || ''"
          :rank="(currentPage - 1) * PAGE_SIZE + i + 1"
        />
      </div>

      <!-- List: two-tier rows (identity row + wrapping telemetry sub-row incl. throughput/
           TTFT/requests as pills), not a <table> -- with 8 columns of unbounded-width
           content (a full multiaddr, several numeric stats) a table forced the row wide
           enough to squeeze/clip the right-hand columns. See PeerListRow.vue. -->
      <div v-else class="w-full max-w-full overflow-x-auto box-border">
        <PeerListRow
          v-for="(p, i) in paginatedPeers"
          :key="p.peer_id || p.node_id || i"
          :peer="p"
          :is-local="(p.peer_id || p.node_id) === nodeInfo.localNodeId"
          :medal="MEDALS[(currentPage - 1) * PAGE_SIZE + i] || ''"
          :rank="(currentPage - 1) * PAGE_SIZE + i + 1"
        />
      </div>

      <!-- Pagination when > 25 -->
      <Pagination
        v-model:currentPage="currentPage"
        :totalPages="totalPages"
        :totalItems="sortedPeers.length"
        :pageSize="PAGE_SIZE"
      />
    </div>

  </div>
</template>
