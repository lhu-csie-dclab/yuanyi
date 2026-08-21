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

  <div class="space-y-4 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <!-- Cluster-wide totals -->
    <div class="card border-brand/30 bg-gradient-to-br from-slate-800/60 to-slate-900/60">
      <h2 class="mb-3 text-xs sm:text-sm font-semibold uppercase tracking-wider text-brand-light flex items-center gap-2">
        <span>🌐</span> Cluster-Wide Contribution
      </h2>
      <div class="grid grid-cols-2 gap-3 sm:gap-4 sm:grid-cols-4">
        <StatCard bare label="Total Tokens" :value="fmtNum(stats.total_cluster_tokens)" accent />
        <StatCard bare label="Input Tokens" :value="fmtNum(stats.total_in_tokens)" />
        <StatCard bare label="Output Tokens" :value="fmtNum(stats.total_out_tokens)" />
        <StatCard bare label="Total Requests" :value="fmtNum(stats.total_cluster_requests)" />
      </div>
    </div>

    <div class="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
      <StatCard label="Nodes Online" :value="stats.total_nodes || 0" />
      <StatCard label="Active Requests" :value="stats.total_active_requests || 0" />
      <StatCard label="Cluster Throughput (tok/s)" :value="(stats.total_gen_speed || 0).toFixed(1)" />
      <StatCard label="Avg TTFT (sec)" :value="(stats.avg_ttft || 0).toFixed(2)" />
    </div>

    <div class="card !p-0 overflow-hidden">
      <div class="flex items-center justify-between px-4 py-4 sm:px-6 sm:pt-6 sm:pb-4">
        <h2 class="text-sm font-semibold text-white">Active Nodes</h2>
        <span class="text-xs text-ink-muted lg:hidden">↔ Scroll horizontally</span>
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
          <tbody>
            <tr v-if="!peers.length">
              <td class="td-cell text-center text-ink-muted py-8" colspan="7">Loading...</td>
            </tr>
            <tr v-for="(p, i) in peers" :key="p.peer_id || i" class="hover:bg-white/[0.02] transition-colors">
              <td class="td-cell font-semibold text-ink-muted">{{ i + 1 }}</td>
              <td class="td-cell"><span class="font-mono text-xs text-brand-light">{{ (p.peer_id || '').substring(0, 12) }}...</span></td>
              <td class="td-cell text-ink-muted font-mono text-xs">{{ p.ip_address || '-' }}</td>
              <td class="td-cell font-semibold">
                <span v-if="!parseGpuInfo(p).summary" class="text-critical text-xs">No GPU</span>
                <span v-else class="text-xs sm:text-sm">{{ parseGpuInfo(p).summary }}</span>
              </td>
              <td class="td-cell">
                <StatusPill v-if="p.fail_count > 0" variant="warning" :label="`RETRY ${p.fail_count}/3`" />
                <StatusPill v-else variant="good" label="HEALTHY" />
                <div class="mt-1 text-[0.7rem] text-ink-faint font-mono">Penalties: {{ p.penalty_points || 0 }}</div>
              </td>
              <td class="td-cell"><TelemetryBadges :info="parseGpuInfo(p)" /></td>
              <td class="td-cell text-xs text-ink-muted font-mono">{{ p.engine_id || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="card">
      <h2 class="mb-3 text-xs sm:text-sm font-semibold text-white flex items-center gap-2">
        <span>🛠️</span> Debug Operations
      </h2>
      <div class="flex flex-wrap gap-2.5 sm:gap-3">
        <button class="btn btn-brand btn-sm sm:btn-base" @click="runDebug(hubForceRank)">Force Rank Update</button>
        <button class="btn btn-critical btn-sm sm:btn-base" @click="runDebug(hubClearOffline)">Clear Offline</button>
      </div>
    </div>
  </div>
</template>
