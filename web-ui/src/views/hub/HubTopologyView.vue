<script setup>
import { ref } from 'vue'
import { getHubStats, getHubPeers, hubForceRank, hubClearOffline, fmtNum, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useToast } from '../../composables/useToast.js'
import PageHeader from '../../components/PageHeader.vue'
import StatCard from '../../components/StatCard.vue'
import StatusPill from '../../components/StatusPill.vue'
import TelemetryBadges from '../../components/TelemetryBadges.vue'

const toast = useToast()
const stats = ref({})
const peers = ref([])

async function refresh() {
  try {
    const [s, p] = await Promise.all([getHubStats(), getHubPeers()])
    stats.value = s
    peers.value = p || []
  } catch {
    /* transient poll failure, next tick retries */
  }
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
  <PageHeader title="Active Topology" :badge="`${stats.total_nodes || 0} Nodes`" />

  <div class="space-y-5 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <!-- Cluster-wide totals -->
    <div class="card border border-blue-100 bg-gradient-to-br from-blue-50/80 via-white to-indigo-50/40 shadow-sm">
      <h2 class="mb-4 text-xs sm:text-sm font-bold uppercase tracking-wider text-brand flex items-center gap-2">
        <span class="flex h-6 w-6 items-center justify-center rounded-md bg-blue-100/80 text-xs">🌐</span>
        Cluster-Wide Contribution
      </h2>
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-4 divide-y sm:divide-y-0 sm:divide-x divide-slate-100">
        <StatCard bare label="Total Tokens" :value="fmtNum(stats.total_cluster_tokens)" accent class="sm:px-3" />
        <StatCard bare label="Input Tokens" :value="fmtNum(stats.total_in_tokens)" class="pt-3 sm:pt-0 sm:px-3" />
        <StatCard bare label="Output Tokens" :value="fmtNum(stats.total_out_tokens)" class="pt-3 sm:pt-0 sm:px-3" />
        <StatCard bare label="Total Requests" :value="fmtNum(stats.total_cluster_requests)" class="pt-3 sm:pt-0 sm:px-3" />
      </div>
    </div>

    <div class="grid grid-cols-2 gap-3.5 sm:gap-5 lg:grid-cols-4">
      <StatCard label="Nodes Online" :value="stats.total_nodes || 0" />
      <StatCard label="Active Requests" :value="stats.total_active_requests || 0" />
      <StatCard label="Cluster Throughput (tok/s)" :value="(stats.total_gen_speed || 0).toFixed(1)" />
      <StatCard label="Avg TTFT (sec)" :value="(stats.avg_ttft || 0).toFixed(2)" />
    </div>

    <div class="card !p-0 overflow-hidden shadow-sm">
      <div class="flex items-center justify-between border-b border-slate-100 px-5 py-4 sm:px-6 sm:py-5 bg-white">
        <div class="flex items-center gap-2.5">
          <span class="flex h-7 w-7 items-center justify-center rounded-lg bg-blue-50 text-brand text-sm font-bold">🛰️</span>
          <h2 class="text-sm sm:text-base font-bold text-slate-800">Active Swarm Nodes</h2>
        </div>
        <span class="text-xs text-slate-400 font-medium lg:hidden">↔ Scroll horizontally</span>
      </div>
      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[700px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell w-12">#</th>
              <th class="th-cell">Peer ID</th>
              <th class="th-cell">IP</th>
              <th class="th-cell">GPU</th>
              <th class="th-cell">Health</th>
              <th class="th-cell">Telemetry</th>
              <th class="th-cell">Engine</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="!peers.length">
              <td class="td-cell text-center text-slate-400 py-10" colspan="7">Loading cluster nodes...</td>
            </tr>
            <tr v-for="(p, i) in peers" :key="p.peer_id || i" class="hover:bg-blue-50/30 transition-colors">
              <td class="td-cell font-bold text-slate-400">{{ i + 1 }}</td>
              <td class="td-cell"><span class="font-mono text-xs font-semibold text-brand">{{ (p.peer_id || '').substring(0, 12) }}...</span></td>
              <td class="td-cell text-slate-500 font-mono text-xs">{{ p.ip_address || '-' }}</td>
              <td class="td-cell font-semibold text-slate-800">
                <span v-if="!parseGpuInfo(p).summary" class="text-rose-500 text-xs font-semibold">No GPU</span>
                <span v-else class="text-xs sm:text-sm">{{ parseGpuInfo(p).summary }}</span>
              </td>
              <td class="td-cell">
                <StatusPill v-if="p.fail_count > 0" variant="warning" :label="`RETRY ${p.fail_count}/3`" />
                <StatusPill v-else variant="good" label="HEALTHY" />
                <div class="mt-1 text-[0.7rem] text-slate-400 font-mono">Penalties: {{ p.penalty_points || 0 }}</div>
              </td>
              <td class="td-cell"><TelemetryBadges :info="parseGpuInfo(p)" /></td>
              <td class="td-cell text-xs text-slate-500 font-mono">{{ p.engine_id || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="card shadow-sm">
      <h2 class="mb-3 text-xs sm:text-sm font-bold text-slate-800 flex items-center gap-2">
        <span>🛠️</span> Hub Administration Operations
      </h2>
      <div class="flex flex-wrap gap-2.5 sm:gap-3">
        <button class="btn btn-brand btn-sm" @click="runDebug(hubForceRank)">Force Rank Update</button>
        <button class="btn btn-critical btn-sm" @click="runDebug(hubClearOffline)">Clear Offline Nodes</button>
      </div>
    </div>
  </div>
</template>
