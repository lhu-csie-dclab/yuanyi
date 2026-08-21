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

const colorPalettes = [
  'bg-amber-400 text-amber-950',
  'bg-rose-400 text-white',
  'bg-emerald-500 text-white',
  'bg-cyan-600 text-white',
  'bg-indigo-500 text-white',
  'bg-orange-500 text-white',
]

function getNodeAvatar(id) {
  if (!id) return { initials: 'ND', color: colorPalettes[0] }
  let sum = 0
  for (let i = 0; i < id.length; i++) sum += id.charCodeAt(i)
  const color = colorPalettes[sum % colorPalettes.length]
  const initials = id.substring(0, 2).toUpperCase()
  return { initials, color }
}

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
  <PageHeader title="Cluster Topology & Rank" :badge="`${stats.total_nodes || 0} Connected`" />

  <div class="space-y-6 p-4 sm:p-6 max-w-full min-w-0">
    <!-- Quick Stats Grid -->
    <div class="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
      <StatCard label="Discovered Peers" :value="stats.total_nodes || 0" />
      <StatCard label="Active Requests" :value="stats.total_active_requests || 0" />
      <StatCard label="Throughput (tok/s)" :value="(stats.total_gen_speed || 0).toFixed(1)" />
      <StatCard label="Avg TTFT (sec)" :value="(stats.avg_ttft || 0).toFixed(2)" />
    </div>

    <!-- My Local Contribution Card with top colored accent strip -->
    <div class="card !p-0 overflow-hidden shadow-xs relative">
      <div class="h-1.5 bg-gradient-to-r from-[#1c5b88] via-[#0284c7] to-[#10b981]" />
      <div class="p-4 sm:p-5">
        <div class="flex items-center justify-between mb-4">
          <div class="flex items-center gap-2">
            <span class="rounded-full bg-blue-50 text-[#1c5b88] px-2.5 py-0.5 text-xs font-bold uppercase">Local Worker</span>
            <h2 class="text-sm font-bold text-slate-800">My Node Contribution &amp; Throughput</h2>
          </div>
          <span class="text-xs text-slate-400 font-mono">Uptime: {{ fmtUptime(local.uptime_seconds) }}</span>
        </div>
        <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5 divide-y sm:divide-y-0 sm:divide-x divide-slate-100">
          <StatCard bare label="Total Tokens" :value="fmtNum(local.total_tokens)" accent class="sm:px-3" />
          <StatCard bare label="Input Tokens" :value="fmtNum(local.in_tokens)" class="pt-2 sm:pt-0 sm:px-3" />
          <StatCard bare label="Output Tokens" :value="fmtNum(local.out_tokens)" class="pt-2 sm:pt-0 sm:px-3" />
          <StatCard bare label="Total Requests" :value="fmtNum(local.total_requests)" class="pt-2 sm:pt-0 sm:px-3" />
          <StatCard bare label="Success Tasks" :value="fmtNum(local.success_count)" class="pt-2 sm:pt-0 sm:px-3" />
        </div>
      </div>
    </div>

    <!-- Main Grid: Left Node Cards & Right Topology Table -->
    <div class="grid grid-cols-1 lg:grid-cols-12 gap-5 items-start">
      <!-- Left: Node Quick Cards (Kanban Style) -->
      <div class="lg:col-span-4 space-y-3">
        <div class="flex items-center justify-between pb-1 border-b border-slate-200">
          <span class="text-xs font-bold uppercase tracking-wider text-slate-500">Resource Nodes</span>
          <span class="text-xs font-mono text-slate-400">{{ peers.length }} nodes</span>
        </div>

        <div v-if="!peers.length" class="card text-center text-slate-400 text-xs py-8">
          Discovering swarm nodes...
        </div>

        <div
          v-for="(p, i) in peers"
          :key="p.peer_id || p.node_id || i"
          class="task-card"
        >
          <!-- Top Accent Stripe -->
          <div
            class="absolute top-0 left-0 right-0 h-1"
            :class="i % 3 === 0 ? 'bg-[#1c5b88]' : (i % 3 === 1 ? 'bg-cyan-500' : 'bg-orange-500')"
          />
          
          <div class="flex items-start justify-between gap-2 mt-1">
            <div class="flex items-center gap-2">
              <span class="rounded bg-sky-50 text-sky-700 px-1.5 py-0.5 text-[0.65rem] font-bold">NODE</span>
              <span class="text-[0.7rem] text-slate-400 font-mono">#{{ (p.peer_id || p.node_id || '').substring(0, 8) }}</span>
            </div>
            <span
              v-if="(p.peer_id || p.node_id) === nodeInfo.localNodeId"
              class="rounded-full bg-[#1c5b88] text-white px-2 py-0.5 text-[0.65rem] font-bold"
            >MY NODE</span>
          </div>

          <div class="mt-2 text-sm font-bold text-slate-800 flex items-center gap-2">
            <div
              class="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[0.7rem] font-bold shadow-2xs"
              :class="getNodeAvatar(p.peer_id || p.node_id).color"
            >
              {{ getNodeAvatar(p.peer_id || p.node_id).initials }}
            </div>
            <span class="truncate">{{ parseGpuInfo(p).summary || 'CPU / General Node' }}</span>
          </div>

          <div class="mt-3 flex items-center justify-between text-xs text-slate-500 border-t border-slate-100 pt-2 font-mono">
            <span>{{ p.ip_address || p.addr || '127.0.0.1' }}</span>
            <span class="text-emerald-600 font-semibold flex items-center gap-1">
              <span class="h-1.5 w-1.5 rounded-full bg-emerald-500" /> Active
            </span>
          </div>
        </div>
      </div>

      <!-- Right: Full Swarm Matrix Table -->
      <div class="lg:col-span-8">
        <div class="card !p-0 overflow-hidden shadow-xs">
          <div class="flex items-center justify-between border-b border-slate-200 px-4 py-3 bg-slate-50">
            <h2 class="text-xs font-bold uppercase tracking-wider text-slate-700">Swarm Assignees &amp; Telemetry Matrix</h2>
            <span class="text-xs text-slate-400 font-medium lg:hidden">↔ Scroll</span>
          </div>
          <div class="overflow-x-auto w-full">
            <table class="w-full min-w-[550px] border-collapse text-left">
              <thead>
                <tr>
                  <th class="th-cell w-10">#</th>
                  <th class="th-cell">Node / Assignee</th>
                  <th class="th-cell">IP</th>
                  <th class="th-cell">Hardware Telemetry</th>
                  <th class="th-cell">Engine</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-slate-100">
                <tr v-if="!peers.length">
                  <td class="td-cell text-center text-slate-400 py-10" colspan="5">Scanning cluster nodes...</td>
                </tr>
                <tr v-for="(p, i) in peers" :key="p.peer_id || p.node_id || i" class="hover:bg-slate-50/80 transition-colors">
                  <td class="td-cell font-bold text-slate-400">{{ i + 1 }}</td>
                  <td class="td-cell">
                    <div class="flex items-center gap-2.5">
                      <div
                        class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-bold shadow-2xs"
                        :class="getNodeAvatar(p.peer_id || p.node_id).color"
                      >
                        {{ getNodeAvatar(p.peer_id || p.node_id).initials }}
                      </div>
                      <div>
                        <div class="font-mono text-xs font-bold text-[#1c5b88]">
                          {{ (p.peer_id || p.node_id || '-').substring(0, 10) }}...
                        </div>
                        <div class="text-[0.7rem] text-slate-500 font-semibold">
                          {{ parseGpuInfo(p).summary || 'Standard Compute' }}
                        </div>
                      </div>
                    </div>
                  </td>
                  <td class="td-cell text-slate-500 font-mono text-xs">{{ p.ip_address || p.addr || '-' }}</td>
                  <td class="td-cell"><TelemetryBadges :info="parseGpuInfo(p)" /></td>
                  <td class="td-cell text-xs text-slate-500 font-mono">{{ p.engine_id || parseGpuInfo(p).engine_id || '-' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
