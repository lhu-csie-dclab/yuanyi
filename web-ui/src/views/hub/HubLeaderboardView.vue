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

  <div class="p-8">
    <div class="card !p-0 overflow-hidden">
      <h2 class="px-6 pt-6 pb-4 text-sm font-semibold">Node Contribution Leaderboard 🏆</h2>
      <div class="overflow-x-auto">
        <table class="w-full border-collapse">
          <thead>
            <tr>
              <th class="th-cell">Rank</th>
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
              <td class="td-cell text-center text-ink-muted" colspan="9">No records</td>
            </tr>
            <tr v-for="(p, i) in board" :key="p.peer_id || i" class="hover:bg-white/[0.02]">
              <td class="td-cell">
                <span v-if="i < 3" class="rounded-lg px-2.5 py-1 text-xs font-bold" :class="medals[i].class">{{ medals[i].label }}</span>
                <span v-else class="font-bold text-ink-muted">#{{ i + 1 }}</span>
              </td>
              <td class="td-cell"><span class="font-mono text-xs text-brand-light">{{ p.peer_id.substring(0, 12) }}...</span></td>
              <td class="td-cell text-ink-muted">{{ p.ip_address || '-' }}</td>
              <td class="td-cell font-semibold">{{ parseGpuInfo(p).summary || 'Unknown' }}</td>
              <td class="td-cell font-bold tabular-nums text-warning">{{ fmtNum(p.total_requests) }}</td>
              <td class="td-cell font-bold tabular-nums text-good">{{ fmtNum(p.in_tokens) }}</td>
              <td class="td-cell font-bold tabular-nums text-brand-light">{{ fmtNum(p.out_tokens) }}</td>
              <td class="td-cell font-bold tabular-nums text-cyan">{{ fmtNum(p.total_tokens) }}</td>
              <td class="td-cell text-[1.05rem] font-bold text-brand-light">{{ (p.contribution_score || 0).toFixed(1) }} pts</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
