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
    <div class="rounded-2xl border border-border bg-surface-card shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3.5 border-b border-white/5 bg-white/[0.03]">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full bg-amber-400" />
          <h2 class="text-sm font-semibold text-ink">{{ t('section_board') }}</h2>
        </div>
        <span class="text-xs text-ink-faint">{{ t('peer_count', board.length) }}</span>
      </div>
      <!--
        Was a 9-column <table>: the IP/multiaddr column has no width cap and 5 more numeric
        columns followed it, so the table forced itself far wider than the viewport and
        clipped the trailing score column. Replaced with two-tier rows: a main row of only
        short, independently-truncatable identity fields, and a sub-row of the numeric
        metrics as pills, free to wrap.
      -->
      <div v-if="!paginatedBoard.length" class="py-12 text-center text-ink-faint text-sm">{{ t('no_records') }}</div>
      <div v-else class="w-full max-w-full overflow-x-auto box-border">
        <div
          v-for="(p, i) in paginatedBoard"
          :key="p.peer_id || i"
          class="flex flex-col gap-2 px-4 py-3 border-b border-white/5 hover:bg-white/5 transition-colors min-w-0 w-full max-w-full box-border"
        >
          <div class="flex items-center gap-3 min-w-0">
            <span class="shrink-0 w-16">
              <span v-if="(currentPage - 1) * PAGE_SIZE + i < 3" class="inline-flex items-center rounded-lg px-2.5 py-1 text-xs font-bold whitespace-nowrap" :class="medals[(currentPage - 1) * PAGE_SIZE + i].class">
                {{ medals[(currentPage - 1) * PAGE_SIZE + i].label }}
              </span>
              <span v-else class="font-bold text-ink-faint text-xs">#{{ (currentPage - 1) * PAGE_SIZE + i + 1 }}</span>
            </span>
            <span
              class="min-w-0 flex-1 font-mono text-xs font-semibold text-brand-light truncate"
              :title="p.peer_id || ''"
            >{{ p.peer_id || '-' }}</span>
            <span
              class="min-w-0 flex-1 font-mono text-xs text-ink-faint truncate"
              :title="p.ip_address || ''"
            >{{ p.ip_address || '-' }}</span>
            <span class="min-w-0 shrink-0 max-w-[35%] text-xs font-semibold text-ink truncate" :title="parseGpuInfo(p).summary || ''">
              {{ parseGpuInfo(p).summary || '-' }}
            </span>
          </div>
          <div class="pl-1 flex flex-wrap items-center gap-2 min-w-0">
            <span class="pill pill-amber">{{ t('col_tasks') }} {{ fmtNum(p.total_requests) }}</span>
            <span class="pill pill-blue">{{ t('col_in_tokens') }} {{ fmtNum(p.in_tokens) }}</span>
            <span class="pill pill-blue">{{ t('col_out_tokens') }} {{ fmtNum(p.out_tokens) }}</span>
            <span class="pill pill-cyan">{{ t('col_total_tokens') }} {{ fmtNum(p.total_tokens) }}</span>
            <span class="pill pill-green">{{ t('col_score') }} {{ (p.contribution_score || 0).toFixed(1) }}</span>
          </div>
        </div>
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
