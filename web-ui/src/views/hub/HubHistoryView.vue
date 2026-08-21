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

  <div class="space-y-4 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <div class="card !p-0 overflow-hidden">
      <div class="flex items-center justify-between px-4 py-4 sm:px-6 sm:pt-6 sm:pb-4">
        <h2 class="text-sm font-semibold text-white">Global Node History &amp; Audit Log</h2>
        <span class="text-xs text-ink-muted lg:hidden">↔ Scroll horizontally</span>
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
          <tbody>
            <tr v-if="!events.length">
              <td class="td-cell text-center text-ink-muted py-8" colspan="7">No events</td>
            </tr>
            <tr v-for="(e, i) in events" :key="e.id ?? i" class="hover:bg-white/[0.02] transition-colors">
              <td class="td-cell text-xs text-ink-muted font-mono">{{ (e.timestamp || '').replace('T', ' ').substring(0, 19) }}</td>
              <td class="td-cell"><StatusPill :variant="metaFor(e.event_type).variant" :label="metaFor(e.event_type).label" /></td>
              <td class="td-cell"><span class="font-mono text-xs text-brand-light">{{ e.peer_id ? e.peer_id.substring(0, 12) + '...' : '-' }}</span></td>
              <td class="td-cell text-ink-muted font-mono text-xs">{{ e.ip_address || '-' }}</td>
              <td class="td-cell text-xs sm:text-sm font-mono">{{ e.fail_count || 0 }}</td>
              <td class="td-cell text-xs sm:text-sm font-mono">{{ e.penalty_points || 0 }}</td>
              <td class="td-cell text-xs text-ink-muted leading-snug">{{ e.detail || '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
