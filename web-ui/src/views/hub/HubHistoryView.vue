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
    <div class="rounded-2xl border border-border bg-surface-card shadow-sm overflow-hidden">
      <div class="flex items-center justify-between px-5 py-3.5 border-b border-white/5">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full bg-brand" />
          <h2 class="text-sm font-semibold text-ink">{{ t('section_events') }}</h2>
        </div>
        <span class="text-xs text-ink-faint">{{ t('records_count', events.length) }}</span>
      </div>
      <!--
        Was a plain <table>: the IP/multiaddr and free-text "detail" columns have no natural
        width cap, so the table forced itself wider than the viewport and squeezed/clipped
        the retries/penalties/detail columns on the right. Replaced with two-tier rows: a
        main row of only short, truncatable identity fields (each independently min-w-0'd so
        one long field can't force the row wide), and a sub-row for the retries/penalties
        pills plus the free-text detail, which is allowed to wrap normally instead.
      -->
      <div v-if="!events.length" class="py-10 text-center text-ink-faint text-sm">{{ t('no_events') }}</div>
      <div v-else class="w-full max-w-full overflow-x-auto box-border">
        <div
          v-for="(e, i) in events"
          :key="e.id ?? i"
          class="flex flex-col gap-2 px-4 py-3 border-b border-white/5 hover:bg-white/5 transition-colors min-w-0 w-full max-w-full box-border"
        >
          <div class="flex items-center gap-3 min-w-0">
            <span class="shrink-0 text-xs text-ink-faint font-mono">{{ (e.timestamp || '').replace('T', ' ').substring(0, 19) }}</span>
            <span class="shrink-0"><StatusPill :variant="metaFor(e.event_type).variant" :label="metaFor(e.event_type).label" /></span>
            <span
              class="min-w-0 flex-1 font-mono text-xs font-semibold text-brand-light truncate"
              :title="e.peer_id || ''"
            >{{ e.peer_id || '-' }}</span>
            <span
              class="min-w-0 flex-1 font-mono text-xs text-ink-faint truncate"
              :title="e.ip_address || ''"
            >{{ e.ip_address || '-' }}</span>
          </div>
          <div class="pl-1 flex flex-wrap items-center gap-2 min-w-0">
            <span class="pill pill-amber">{{ t('col_retries') }}: {{ e.fail_count || 0 }}</span>
            <span class="pill pill-red">{{ t('col_penalties') }}: {{ e.penalty_points || 0 }}</span>
            <span v-if="e.detail" class="text-xs text-ink-faint leading-snug break-words min-w-0">{{ e.detail }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
