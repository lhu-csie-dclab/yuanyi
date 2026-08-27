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
import PeerListRow from '../../components/PeerListRow.vue'

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
    <div class="rounded-2xl border border-brand/25 bg-gradient-to-r from-brand/10 to-cyan/5 p-5">
      <div class="mb-4 text-xs font-bold uppercase tracking-widest text-brand-light">{{ t('section_cluster') }}</div>
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-4 divide-x divide-white/10">
        <StatCard bare :label="t('stat_cluster_tokens')" :value="fmtNum(stats.total_cluster_tokens)" accent />
        <StatCard bare :label="t('stat_input')"          :value="fmtNum(stats.total_in_tokens)"      class="pl-4" />
        <StatCard bare :label="t('stat_output')"         :value="fmtNum(stats.total_out_tokens)"     class="pl-4" />
        <StatCard bare :label="t('stat_requests')"       :value="fmtNum(stats.total_cluster_requests)" class="pl-4" />
      </div>
    </div>

    <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <StatCard icon="🛰️" color="bg-brand/10"    :label="t('stat_nodes')"      :value="stats.total_nodes || 0" accent />
      <StatCard icon="⚡"  color="bg-violet-400/10"  :label="t('stat_requests')"   :value="stats.total_active_requests || 0" />
      <StatCard icon="📈" color="bg-emerald-400/10"  :label="t('stat_throughput')" :value="(stats.total_gen_speed || 0).toFixed(1)" />
      <StatCard icon="⏱"  color="bg-amber-400/10"   :label="t('stat_ttft')"       :value="(stats.avg_ttft || 0).toFixed(2)" />
    </div>

    <!-- Node list -->
    <div class="rounded-2xl border border-border bg-surface-card shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3.5 border-b border-white/5 bg-white/[0.03]">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
          <h2 class="text-sm font-semibold text-ink">{{ t('section_node_list') }}</h2>
        </div>
        <span class="text-xs text-ink-faint">{{ t('peer_count', peers.length) }}</span>
      </div>
      <div v-if="!paginatedPeers.length" class="py-12 text-center text-ink-faint text-sm">{{ t('loading_cluster') }}</div>
      <div v-else class="w-full max-w-full overflow-x-auto box-border">
        <PeerListRow
          v-for="(p, i) in paginatedPeers"
          :key="p.peer_id || i"
          :peer="p"
          :rank="(currentPage - 1) * PAGE_SIZE + i + 1"
        >
          <template #extra>
            <StatusPill v-if="p.fail_count > 0" variant="warning" :label="t('retry_label', p.fail_count)" />
            <StatusPill v-else variant="good" :label="t('status_healthy')" />
          </template>
          <template #sub-extra>
            <span v-if="p.penalty_points" class="text-[0.68rem] text-rose-400 font-mono">{{ t('penalty', p.penalty_points) }}</span>
            <span v-if="p.engine_id" class="text-[0.68rem] text-ink-faint font-mono">engine: {{ p.engine_id }}</span>
          </template>
        </PeerListRow>
      </div>

      <Pagination
        v-model:currentPage="currentPage"
        :totalPages="totalPages"
        :totalItems="peers.length"
        :pageSize="PAGE_SIZE"
      />
    </div>

    <!-- Admin -->
    <div class="rounded-2xl border border-border bg-surface-card p-4 shadow-sm">
      <h2 class="mb-3 text-xs font-bold uppercase tracking-widest text-ink-faint">🛠️ {{ t('hub_admin') }}</h2>
      <div class="flex flex-wrap gap-2">
        <button class="btn btn-brand btn-sm" @click="runDebug(hubForceRank)">{{ t('btn_force_rank') }}</button>
        <button class="btn btn-critical btn-sm" @click="runDebug(hubClearOffline)">{{ t('btn_clear_offline') }}</button>
      </div>
    </div>
  </div>
</template>
