<script setup>
import { ref } from 'vue'
import { getHubEvents } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import { useI18n } from '../../composables/useI18n.js'
import PageHeader from '../../components/PageHeader.vue'
import StatusPill from '../../components/StatusPill.vue'

const { t } = useI18n()
const events = ref([])

function eventMeta(type) {
  const map = {
    FAIL_INCREMENT:    { variant: 'warning',  key: 'evt_fail' },
    RECOVERED_PENALTY: { variant: 'good',     key: 'evt_recover' },
    WARNING_OFFLINE:   { variant: 'critical', key: 'evt_offline' },
    JOIN:              { variant: 'good',     key: 'evt_join' },
  }
  return map[type] || { variant: 'good', key: null }
}

function metaFor(type) {
  const m = eventMeta(type)
  return { variant: m.variant, label: m.key ? t(m.key) : type }
}

async function refresh() {
  try { events.value = (await getHubEvents()) || [] } catch {}
}
usePolling(refresh, 3000)
</script>

<template>
  <PageHeader :title="t('page_history')" />

  <div class="p-5 max-w-full min-w-0">
    <div class="rounded-2xl border border-slate-200/80 bg-white shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3.5 border-b border-slate-100">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full bg-blue-400" />
          <h2 class="text-sm font-semibold text-slate-800">{{ t('section_events') }}</h2>
        </div>
        <span class="text-xs text-slate-400">{{ t('records_count', events.length) }}</span>
      </div>
      <div class="overflow-x-auto w-full">
        <table class="w-full min-w-[600px] border-collapse text-left">
          <thead>
            <tr>
              <th class="th-cell">{{ t('col_time') }}</th>
              <th class="th-cell">{{ t('col_event') }}</th>
              <th class="th-cell">{{ t('col_node_id') }}</th>
              <th class="th-cell">{{ t('col_ip') }}</th>
              <th class="th-cell">{{ t('col_retries') }}</th>
              <th class="th-cell">{{ t('col_penalties') }}</th>
              <th class="th-cell">{{ t('col_detail') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100">
            <tr v-if="!events.length">
              <td class="td-cell text-center text-slate-400 py-10" colspan="7">{{ t('no_events') }}</td>
            </tr>
            <tr v-for="(e, i) in events" :key="e.id ?? i" class="hover:bg-slate-50/70 transition-colors">
              <td class="td-cell text-xs text-slate-500 font-mono">{{ (e.timestamp || '').replace('T', ' ').substring(0, 19) }}</td>
              <td class="td-cell"><StatusPill :variant="metaFor(e.event_type).variant" :label="metaFor(e.event_type).label" /></td>
              <td class="td-cell font-mono text-xs font-semibold text-blue-600">{{ e.peer_id ? e.peer_id.substring(0, 12) + '…' : '-' }}</td>
              <td class="td-cell text-slate-500 font-mono text-xs">{{ e.ip_address || '-' }}</td>
              <td class="td-cell text-xs font-mono font-semibold text-slate-700">{{ e.fail_count || 0 }}</td>
              <td class="td-cell text-xs font-mono font-semibold text-rose-500">{{ e.penalty_points || 0 }}</td>
              <td class="td-cell text-xs text-slate-500 leading-snug">{{ e.detail || '' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
