<script setup>
import { ref, computed } from 'vue'
import { getHubStats, getHubPeers, hubForceRank, hubClearOffline, fmtNum, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useToast } from '../../composables/useToast.js'
import { useI18n } from '../../composables/useI18n.js'
import PageHeader from '../../components/PageHeader.vue'
import StatCard from '../../components/StatCard.vue'
import StatusPill from '../../components/StatusPill.vue'
import Pagination from '../../components/Pagination.vue'
import TelemetryBadges from '../../components/TelemetryBadges.vue'

const toast = useToast()
const { t } = useI18n()
const stats = ref({})
const peers = ref([])

// Pagination (25 per page)
const PAGE_SIZE = 25
const currentPage = ref(1)

const totalPages = computed(() => Math.max(1, Math.ceil(peers.value.length / PAGE_SIZE)))

const paginatedPeers = computed(() => {
  const start = (currentPage.value - 1) * PAGE_SIZE
  return peers.value.slice(start, start + PAGE_SIZE)
})

async function refresh() {
  try {
    const [s, p] = await Promise.all([getHubStats(), getHubPeers()])
    stats.value = s
    peers.value = p || []
  } catch {}
}
usePolling(refresh, 2000)

async function runDebug(fn) {
  try {
    const d = await fn()
    toast.success(d.message || 'OK')
    refresh()
  } catch {
    toast.error('Failed')
  }
}
</script>

<template>
  <PageHeader :title="t('page_hub_topology')" :badge="t('badge_nodes', stats.total_nodes || 0)" />

  <div class="space-y-5 p-5 max-w-full min-w-0">
    <!-- Cluster totals -->
    <div class="rounded-2xl border border-blue-200/60 bg-gradient-to-r from-blue-50 to-indigo-50/40 p-5">
      <div class="mb-4 text-xs font-bold uppercase tracking-widest text-blue-600">{{ t('section_cluster') }}</div>
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-4 divide-x divide-blue-100">
        <StatCard bare :label="t('stat_cluster_tokens')" :value="fmtNum(stats.total_cluster_tokens)" accent />
        <StatCard bare :label="t('stat_input')"          :value="fmtNum(stats.total_in_tokens)"      class="pl-4" />
        <StatCard bare :label="t('stat_output')"         :value="fmtNum(stats.total_out_tokens)"     class="pl-4" />
        <StatCard bare :label="t('stat_requests')"       :value="fmtNum(stats.total_cluster_requests)" class="pl-4" />
      </div>
    </div>

    <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <StatCard icon="🛰️" color="bg-blue-50"    :label="t('stat_nodes')"      :value="stats.total_nodes || 0" accent />
      <StatCard icon="⚡"  color="bg-violet-50"  :label="t('stat_requests')"   :value="stats.total_active_requests || 0" />
      <StatCard icon="📈" color="bg-emerald-50"  :label="t('stat_throughput')" :value="(stats.total_gen_speed || 0).toFixed(1)" />
      <StatCard icon="⏱"  color="bg-amber-50"   :label="t('stat_ttft')"       :value="(stats.avg_ttft || 0).toFixed(2)" />
    </div>

    <!-- Node list -->
    <div class="rounded-2xl border border-slate-200/80 bg-white shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3.5 border-b border-slate-100 bg-slate-50/50">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
          <h2 class="text-sm font-semibold text-slate-800">{{ t('section_node_list') }}</h2>
        </div>
        <span class="text-xs text-slate-400">{{ t('peer_count', peers.length) }}</span>
      </div>
      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell w-12">{{ t('col_rank') }}</th>
              <th class="th-cell">{{ t('col_node_id') }}</th>
              <th class="th-cell">{{ t('col_ip') }}</th>
              <th class="th-cell">{{ t('col_gpu') }}</th>
              <th class="th-cell">{{ t('col_status') }}</th>
              <th class="th-cell">{{ t('col_telemetry') }}</th>
              <th class="th-cell">{{ t('col_engine') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="!paginatedPeers.length">
              <td class="td-cell text-center text-slate-400 py-12" colspan="7">{{ t('loading_cluster') }}</td>
            </tr>
            <tr v-for="(p, i) in paginatedPeers" :key="p.peer_id || i" class="hover:bg-slate-50/70 transition-colors">
              <td class="td-cell text-slate-400 font-semibold">{{ (currentPage - 1) * PAGE_SIZE + i + 1 }}</td>
              <td class="td-cell font-mono text-xs font-semibold text-blue-600">{{ (p.peer_id || '').substring(0, 12) }}…</td>
              <td class="td-cell text-slate-500 font-mono text-xs">{{ p.ip_address || '-' }}</td>
              <td class="td-cell text-xs font-semibold text-slate-800">
                <span v-if="!parseGpuInfo(p).summary" class="pill pill-red">{{ t('no_gpu') }}</span>
                <span v-else>{{ parseGpuInfo(p).summary }}</span>
              </td>
              <td class="td-cell">
                <StatusPill v-if="p.fail_count > 0" variant="warning" :label="t('retry_label', p.fail_count)" />
                <StatusPill v-else variant="good" :label="t('status_healthy')" />
                <div v-if="p.penalty_points" class="mt-0.5 text-[0.68rem] text-rose-400 font-mono">{{ t('penalty', p.penalty_points) }}</div>
              </td>
              <td class="td-cell"><TelemetryBadges :info="parseGpuInfo(p)" /></td>
              <td class="td-cell text-xs text-slate-400 font-mono">{{ p.engine_id || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <Pagination
        v-model:currentPage="currentPage"
        :totalPages="totalPages"
        :totalItems="peers.length"
        :pageSize="PAGE_SIZE"
      />
    </div>

    <!-- Admin -->
    <div class="rounded-2xl border border-slate-200/80 bg-white p-4 shadow-sm">
      <h2 class="mb-3 text-xs font-bold uppercase tracking-widest text-slate-400">🛠️ {{ t('hub_admin') }}</h2>
      <div class="flex flex-wrap gap-2">
        <button class="btn btn-brand btn-sm" @click="runDebug(hubForceRank)">{{ t('btn_force_rank') }}</button>
        <button class="btn btn-critical btn-sm" @click="runDebug(hubClearOffline)">{{ t('btn_clear_offline') }}</button>
      </div>
    </div>
  </div>
</template>
