<script setup>
import { ref } from 'vue'
import { getHubLeaderboard, fmtNum, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import PageHeader from '../../components/PageHeader.vue'

const board = ref([])

const medals = [
  { label: '🏆 MVP', class: 'bg-gradient-to-br from-amber-500 to-amber-600 text-white shadow-[0_0_12px_rgba(245,158,11,0.4)]' },
  { label: '🥈 #2', class: 'bg-gradient-to-br from-slate-400 to-slate-500 text-white' },
  { label: '🥉 #3', class: 'bg-gradient-to-br from-amber-800 to-amber-900 text-white' },
]

async function refresh() {
  try {
    board.value = (await getHubLeaderboard()) || []
  } catch {
    /* transient poll failure, next tick retries */
  }
}

usePolling(refresh, 3000)
</script>

<template>
  <PageHeader title="Leaderboard" />

  <div class="space-y-4 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <div class="card !p-0 overflow-hidden">
      <div class="flex items-center justify-between px-4 py-4 sm:px-6 sm:pt-6 sm:pb-4">
        <h2 class="text-sm font-semibold text-white">Node Contribution Leaderboard 🏆</h2>
        <span class="text-xs text-ink-muted lg:hidden">↔ Scroll horizontally</span>
      </div>
      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[760px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell w-16">Rank</th>
              <th class="th-cell">Peer</th>
              <th class="th-cell">IP</th>
              <th class="th-cell">GPU</th>
              <th class="th-cell">Tasks</th>
              <th class="th-cell">In Tokens</th>
              <th class="th-cell">Out Tokens</th>
              <th class="th-cell">Total Tokens</th>
              <th class="th-cell">Score</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!board.length">
              <td class="td-cell text-center text-ink-muted py-8" colspan="9">No records</td>
            </tr>
            <tr v-for="(p, i) in board" :key="p.peer_id || i" class="hover:bg-white/[0.02] transition-colors">
              <td class="td-cell">
                <span v-if="i < 3" class="rounded-lg px-2.5 py-1 text-xs font-bold shadow-sm" :class="medals[i].class">{{ medals[i].label }}</span>
                <span v-else class="font-bold text-ink-muted text-xs">#{{ i + 1 }}</span>
              </td>
              <td class="td-cell"><span class="font-mono text-xs text-brand-light">{{ (p.peer_id || '').substring(0, 12) }}...</span></td>
              <td class="td-cell text-ink-muted font-mono text-xs">{{ p.ip_address || '-' }}</td>
              <td class="td-cell font-semibold text-xs sm:text-sm">{{ parseGpuInfo(p).summary || 'Unknown' }}</td>
              <td class="td-cell font-bold tabular-nums text-warning text-xs sm:text-sm">{{ fmtNum(p.total_requests) }}</td>
              <td class="td-cell font-bold tabular-nums text-good text-xs sm:text-sm">{{ fmtNum(p.in_tokens) }}</td>
              <td class="td-cell font-bold tabular-nums text-brand-light text-xs sm:text-sm">{{ fmtNum(p.out_tokens) }}</td>
              <td class="td-cell font-bold tabular-nums text-cyan text-xs sm:text-sm">{{ fmtNum(p.total_tokens) }}</td>
              <td class="td-cell text-xs sm:text-sm font-bold text-brand-light">{{ (p.contribution_score || 0).toFixed(1) }} pts</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
