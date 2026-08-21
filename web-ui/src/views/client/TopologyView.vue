<script setup>
import { ref } from 'vue'
import { getClusterStats, getPeers, getLocalStats, fmtNum, fmtUptime, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useNodeInfo } from '../../composables/useNodeInfo.js'
import PageHeader from '../../components/PageHeader.vue'
import StatCard from '../../components/StatCard.vue'
import TelemetryBadges from '../../components/TelemetryBadges.vue'

const nodeInfo = useNodeInfo()

const stats = ref({ total_nodes: 0, total_active_requests: 0, total_gen_speed: 0, avg_ttft: 0 })
const peers = ref([])
const local = ref({ total_tokens: 0, in_tokens: 0, out_tokens: 0, total_requests: 0, success_count: 0, uptime_seconds: 0 })

async function refresh() {
  try {
    const [s, p] = await Promise.all([getClusterStats(), getPeers()])
    stats.value = s
    peers.value = p || []
  } catch {
    /* transient poll failure, next tick retries */
  }
  try {
    local.value = await getLocalStats()
  } catch {
    /* central leaderboard cross-check is best-effort */
  }
}

usePolling(refresh, 2000)
</script>

<template>
  <PageHeader title="Topology & Rank" :badge="`${stats.total_nodes || 0} Peers`" />

  <div class="space-y-4 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <!-- My Local Contribution -->
    <div class="card border border-blue-100 bg-gradient-to-br from-blue-50/70 via-white to-indigo-50/30 shadow-xs">
      <h2 class="mb-3 text-xs sm:text-sm font-bold uppercase tracking-wider text-blue-700 flex items-center gap-2">
        <span>📊</span> My Local Contribution
      </h2>
      <div class="grid grid-cols-2 gap-3 sm:gap-4 sm:grid-cols-3 lg:grid-cols-6">
        <StatCard bare label="Total Tokens" :value="fmtNum(local.total_tokens)" accent />
        <StatCard bare label="Input Tokens" :value="fmtNum(local.in_tokens)" />
        <StatCard bare label="Output Tokens" :value="fmtNum(local.out_tokens)" />
        <StatCard bare label="Requests" :value="fmtNum(local.total_requests)" />
        <StatCard bare label="Success" :value="fmtNum(local.success_count)" />
        <StatCard bare label="Uptime" :value="fmtUptime(local.uptime_seconds)" />
      </div>
    </div>

    <!-- Swarm-wide stats -->
    <div class="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
      <StatCard label="Discovered Peers" :value="stats.total_nodes || 0" />
      <StatCard label="Active Requests" :value="stats.total_active_requests || 0" />
      <StatCard label="Throughput (tok/s)" :value="(stats.total_gen_speed || 0).toFixed(1)" />
      <StatCard label="Avg TTFT (sec)" :value="(stats.avg_ttft || 0).toFixed(2)" />
    </div>

    <!-- Peers table -->
    <div class="card !p-0 overflow-hidden shadow-xs">
      <div class="flex items-center justify-between px-4 py-4 sm:px-6 sm:pt-6 sm:pb-4 border-b border-slate-100 bg-white">
        <h2 class="text-sm sm:text-base font-bold text-slate-800">P2P Discovered Peers</h2>
        <span class="text-xs text-slate-400 font-medium lg:hidden">↔ Scroll horizontally</span>
      </div>
      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell w-12">#</th>
              <th class="th-cell">Peer ID</th>
              <th class="th-cell">IP Address</th>
              <th class="th-cell">GPU Model</th>
              <th class="th-cell">Hardware Telemetry</th>
              <th class="th-cell">Engine</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="!peers.length">
              <td class="td-cell text-center text-slate-400 py-10" colspan="6">Discovering peers...</td>
            </tr>
            <tr v-for="(p, i) in peers" :key="p.peer_id || p.node_id || i" class="hover:bg-blue-50/30 transition-colors">
              <td class="td-cell font-bold text-slate-400">{{ i + 1 }}</td>
              <td class="td-cell">
                <span class="font-mono text-xs font-semibold text-blue-600">{{ (p.peer_id || p.node_id || '-').substring(0, 12) }}...</span>
                <span
                  v-if="(p.peer_id || p.node_id) === nodeInfo.localNodeId"
                  class="ml-1.5 rounded-md bg-blue-600 px-1.5 py-0.5 text-[0.65rem] font-bold text-white shadow-2xs"
                >ME</span>
              </td>
              <td class="td-cell text-slate-500 font-mono text-xs">{{ p.ip_address || p.addr || '-' }}</td>
              <td class="td-cell font-semibold text-slate-800">
                <span v-if="!parseGpuInfo(p).summary" class="text-rose-500 text-xs font-semibold">No GPU</span>
                <span v-else class="text-xs sm:text-sm">{{ parseGpuInfo(p).summary }}</span>
              </td>
              <td class="td-cell"><TelemetryBadges :info="parseGpuInfo(p)" /></td>
              <td class="td-cell text-xs text-slate-500 font-mono">{{ p.engine_id || parseGpuInfo(p).engine_id || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

