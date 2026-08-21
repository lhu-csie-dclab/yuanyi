<script setup>
import { ref, computed } from 'vue'
import { getClusterStats, getPeers, fmtNum, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useNodeInfo } from '../../composables/useNodeInfo.js'
import { useI18n } from '../../composables/useI18n.js'
import PageHeader from '../../components/PageHeader.vue'
import Pagination from '../../components/Pagination.vue'
import TelemetryBadges from '../../components/TelemetryBadges.vue'

const nodeInfo = useNodeInfo()
const { t } = useI18n()
const stats = ref({ total_nodes: 0, total_active_requests: 0, total_gen_speed: 0, avg_ttft: 0 })
const peers = ref([])

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
    <div class="rounded-2xl bg-white border border-slate-200/80 shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3 border-b border-slate-100 bg-slate-50/50">
        <div class="flex items-center gap-2">
          <span class="text-base">🌐</span>
          <span class="text-xs font-bold uppercase tracking-widest text-slate-700">{{ t('section_cluster') || 'Cluster Summary' }}</span>
        </div>
        <span class="pill pill-green"><span class="h-1.5 w-1.5 rounded-full bg-emerald-400 inline-block mr-1"/>{{ t('status_online') }}</span>
      </div>
      <div class="grid grid-cols-2 sm:grid-cols-4 divide-x divide-slate-100">
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-slate-400">{{ t('stat_nodes') }}</span>
          <span class="text-lg font-bold text-blue-600 tabular-nums">{{ stats.total_nodes || 0 }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-slate-400">{{ t('stat_requests') }}</span>
          <span class="text-lg font-bold text-violet-600 tabular-nums">{{ stats.total_active_requests || 0 }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-slate-400">{{ t('stat_throughput') }}</span>
          <span class="text-lg font-bold text-emerald-600 tabular-nums">{{ (stats.total_gen_speed || 0).toFixed(1) }}</span>
        </div>
        <div class="px-4 py-3 flex flex-col">
          <span class="text-[0.65rem] font-bold uppercase tracking-wider text-slate-400">{{ t('stat_ttft') }}</span>
          <span class="text-lg font-bold text-amber-600 tabular-nums">{{ (stats.avg_ttft || 0).toFixed(2) }}s</span>
        </div>
      </div>
    </div>

    <!-- ② TOP Ranking Table (Paginated by 25) -->
    <div class="rounded-2xl border border-slate-200/80 bg-white shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3.5 border-b border-slate-100 bg-slate-50/50">
        <div class="flex items-center gap-2">
          <span class="text-base">🏆</span>
          <h2 class="text-sm font-semibold text-slate-800">{{ t('top_nodes') }}</h2>
        </div>
        <div class="flex items-center gap-2 text-xs text-slate-400">
          <span>{{ t('by_throughput') }}</span>
          <span>·</span>
          <span>共 {{ sortedPeers.length }} 節點</span>
        </div>
      </div>

      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell w-16">{{ t('col_rank') }}</th>
              <th class="th-cell">{{ t('col_node_id') }}</th>
              <th class="th-cell">{{ t('col_ip') }}</th>
              <th class="th-cell">{{ t('col_gpu') }}</th>
              <th class="th-cell">{{ t('stat_throughput') }}</th>
              <th class="th-cell">{{ t('stat_ttft') }}</th>
              <th class="th-cell">{{ t('stat_requests') }}</th>
              <th class="th-cell">{{ t('col_telemetry') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="!paginatedPeers.length">
              <td class="td-cell py-12 text-center text-slate-400" colspan="8">{{ t('loading_nodes') }}</td>
            </tr>
            <tr
              v-for="(p, i) in paginatedPeers"
              :key="p.peer_id || p.node_id || i"
              class="hover:bg-slate-50/70 transition-colors"
            >
              <td class="td-cell font-bold">
                <template v-if="(currentPage - 1) * PAGE_SIZE + i === 0">
                  <span class="text-amber-500 text-base">🥇</span>
                </template>
                <template v-else-if="(currentPage - 1) * PAGE_SIZE + i === 1">
                  <span class="text-slate-400 text-base">🥈</span>
                </template>
                <template v-else-if="(currentPage - 1) * PAGE_SIZE + i === 2">
                  <span class="text-amber-700 text-base">🥉</span>
                </template>
                <template v-else>
                  <span class="text-slate-400 text-xs font-semibold pl-1">#{{ (currentPage - 1) * PAGE_SIZE + i + 1 }}</span>
                </template>
              </td>
              <td class="td-cell">
                <div class="flex items-center gap-2">
                  <div class="h-7 w-7 shrink-0 rounded-lg bg-blue-600 flex items-center justify-center text-white text-xs font-bold">
                    {{ (p.peer_id || p.node_id || 'ND').substring(0, 2).toUpperCase() }}
                  </div>
                  <div>
                    <div class="font-mono text-xs font-semibold text-blue-600">
                      {{ (p.peer_id || p.node_id || '-').substring(0, 12) }}…
                    </div>
                    <div v-if="(p.peer_id || p.node_id) === nodeInfo.localNodeId" class="pill pill-blue text-[0.6rem] mt-0.5">{{ t('local_badge') }}</div>
                  </div>
                </div>
              </td>
              <td class="td-cell font-mono text-xs text-slate-500">{{ p.ip_address || p.addr || '-' }}</td>
              <td class="td-cell font-semibold text-slate-800 text-xs">
                <span v-if="!p._gpu.summary" class="pill pill-red">{{ t('no_gpu') }}</span>
                <span v-else>{{ p._gpu.summary }}</span>
              </td>
              <td class="td-cell font-bold tabular-nums text-emerald-600 text-sm">
                {{ (p._gpu.gen_speed || 0).toFixed(1) }}
              </td>
              <td class="td-cell font-mono text-xs text-slate-500">
                {{ (p._gpu.avg_ttft || 0).toFixed(2) }}s
              </td>
              <td class="td-cell font-bold tabular-nums text-violet-600 text-sm">
                {{ p._gpu.active_requests || 0 }}
              </td>
              <td class="td-cell">
                <TelemetryBadges :info="p._gpu" />
              </td>
            </tr>
          </tbody>
        </table>
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
