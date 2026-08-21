<script setup>
import { ref, computed } from 'vue'
import { getHubLeaderboard, fmtNum, parseGpuInfo } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useI18n } from '../../composables/useI18n.js'
import PageHeader from '../../components/PageHeader.vue'
import Pagination from '../../components/Pagination.vue'

const { t } = useI18n()
const board = ref([])

// Pagination (25 per page)
const PAGE_SIZE = 25
const currentPage = ref(1)

const totalPages = computed(() => Math.max(1, Math.ceil(board.value.length / PAGE_SIZE)))

const paginatedBoard = computed(() => {
  const start = (currentPage.value - 1) * PAGE_SIZE
  return board.value.slice(start, start + PAGE_SIZE)
})

const medals = [
  { label: '🏆 MVP', class: 'bg-gradient-to-br from-amber-500 to-amber-600 text-white shadow-[0_0_12px_rgba(245,158,11,0.4)]' },
  { label: '🥈 #2',  class: 'bg-gradient-to-br from-slate-400 to-slate-500 text-white' },
  { label: '🥉 #3',  class: 'bg-gradient-to-br from-amber-800 to-amber-900 text-white' },
]

async function refresh() {
  try { board.value = (await getHubLeaderboard()) || [] } catch {}
}
usePolling(refresh, 3000)
</script>

<template>
  <PageHeader :title="t('page_leaderboard')" />

  <div class="p-5 max-w-full min-w-0">
    <div class="rounded-2xl border border-slate-200/80 bg-white shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3.5 border-b border-slate-100 bg-slate-50/50">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full bg-amber-400" />
          <h2 class="text-sm font-semibold text-slate-800">{{ t('section_board') }}</h2>
        </div>
        <span class="text-xs text-slate-400">{{ t('peer_count', board.length) }}</span>
      </div>
      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[640px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell w-20">{{ t('col_rank_label') }}</th>
              <th class="th-cell">{{ t('col_peer') }}</th>
              <th class="th-cell">{{ t('col_ip') }}</th>
              <th class="th-cell">{{ t('col_gpu') }}</th>
              <th class="th-cell">{{ t('col_tasks') }}</th>
              <th class="th-cell">{{ t('col_in_tokens') }}</th>
              <th class="th-cell">{{ t('col_out_tokens') }}</th>
              <th class="th-cell">{{ t('col_total_tokens') }}</th>
              <th class="th-cell">{{ t('col_score') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="!paginatedBoard.length">
              <td class="td-cell text-center text-slate-400 py-12" colspan="9">{{ t('no_records') }}</td>
            </tr>
            <tr v-for="(p, i) in paginatedBoard" :key="p.peer_id || i" class="hover:bg-slate-50/70 transition-colors">
              <td class="td-cell">
                <span v-if="(currentPage - 1) * PAGE_SIZE + i < 3" class="inline-flex items-center rounded-lg px-2.5 py-1 text-xs font-bold" :class="medals[(currentPage - 1) * PAGE_SIZE + i].class">
                  {{ medals[(currentPage - 1) * PAGE_SIZE + i].label }}
                </span>
                <span v-else class="font-bold text-slate-400 text-xs ml-2">#{{ (currentPage - 1) * PAGE_SIZE + i + 1 }}</span>
              </td>
              <td class="td-cell font-mono text-xs font-semibold text-blue-600">{{ (p.peer_id || '').substring(0, 12) }}…</td>
              <td class="td-cell text-slate-500 font-mono text-xs">{{ p.ip_address || '-' }}</td>
              <td class="td-cell text-xs font-semibold text-slate-800">{{ parseGpuInfo(p).summary || '-' }}</td>
              <td class="td-cell font-bold tabular-nums text-amber-600 text-xs">{{ fmtNum(p.total_requests) }}</td>
              <td class="td-cell font-bold tabular-nums text-slate-600 text-xs">{{ fmtNum(p.in_tokens) }}</td>
              <td class="td-cell font-bold tabular-nums text-blue-600 text-xs">{{ fmtNum(p.out_tokens) }}</td>
              <td class="td-cell font-bold tabular-nums text-cyan-600 text-xs">{{ fmtNum(p.total_tokens) }}</td>
              <td class="td-cell text-xs font-bold text-blue-700">{{ (p.contribution_score || 0).toFixed(1) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <Pagination
        v-model:currentPage="currentPage"
        :totalPages="totalPages"
        :totalItems="board.length"
        :pageSize="PAGE_SIZE"
      />
    </div>
  </div>
</template>
