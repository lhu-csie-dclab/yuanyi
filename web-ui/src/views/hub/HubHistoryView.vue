<script setup>
import { ref } from 'vue'
import { getHubEvents } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import PageHeader from '../../components/PageHeader.vue'
import StatusPill from '../../components/StatusPill.vue'

const events = ref([])

const eventMeta = {
  FAIL_INCREMENT: { variant: 'warning', label: 'FAIL RETRY' },
  RECOVERED_PENALTY: { variant: 'good', label: 'RECOVERED (+1)' },
  WARNING_OFFLINE: { variant: 'critical', label: 'OFFLINE WARNING' },
  JOIN: { variant: 'good', label: 'JOINED' },
}

function metaFor(type) {
  return eventMeta[type] || { variant: 'good', label: type }
}

async function refresh() {
  try {
    events.value = (await getHubEvents()) || []
  } catch {
    /* transient poll failure, next tick retries */
  }
}

usePolling(refresh, 3000)
</script>

<template>
  <PageHeader title="Global History" />

  <div class="space-y-5 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <div class="card !p-0 overflow-hidden shadow-sm">
      <div class="flex items-center justify-between border-b border-slate-100 px-5 py-4 sm:px-6 sm:py-5 bg-white">
        <div class="flex items-center gap-2.5">
          <span class="flex h-7 w-7 items-center justify-center rounded-lg bg-blue-50 text-brand text-sm font-bold">📋</span>
          <h2 class="text-sm sm:text-base font-bold text-slate-800">Global Node History &amp; Audit Log</h2>
        </div>
        <span class="text-xs text-slate-400 font-medium lg:hidden">↔ Scroll horizontally</span>
      </div>
      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[700px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell">Timestamp</th>
              <th class="th-cell">Event</th>
              <th class="th-cell">Peer</th>
              <th class="th-cell">IP</th>
              <th class="th-cell">Retries</th>
              <th class="th-cell">Penalties</th>
              <th class="th-cell">Detail</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="!events.length">
              <td class="td-cell text-center text-slate-400 py-10" colspan="7">No audit events recorded</td>
            </tr>
            <tr v-for="(e, i) in events" :key="e.id ?? i" class="hover:bg-blue-50/30 transition-colors">
              <td class="td-cell text-xs text-slate-500 font-mono">{{ (e.timestamp || '').replace('T', ' ').substring(0, 19) }}</td>
              <td class="td-cell"><StatusPill :variant="metaFor(e.event_type).variant" :label="metaFor(e.event_type).label" /></td>
              <td class="td-cell"><span class="font-mono text-xs font-semibold text-brand">{{ e.peer_id ? e.peer_id.substring(0, 12) + '...' : '-' }}</span></td>
              <td class="td-cell text-slate-500 font-mono text-xs">{{ e.ip_address || '-' }}</td>
              <td class="td-cell text-xs sm:text-sm font-mono font-semibold text-slate-700">{{ e.fail_count || 0 }}</td>
              <td class="td-cell text-xs sm:text-sm font-mono font-semibold text-rose-500">{{ e.penalty_points || 0 }}</td>
              <td class="td-cell text-xs text-slate-600 leading-snug">{{ e.detail || '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
