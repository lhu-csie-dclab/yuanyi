<script setup>
import { parseGpuInfo } from '../api.js'
import { useI18n } from '../composables/useI18n.js'
import TelemetryBadges from './TelemetryBadges.vue'

const props = defineProps({
  peer: { type: Object, required: true },
  isLocal: { type: Boolean, default: false },
  medal: { type: String, default: '' },
  rank: { type: Number, default: 0 },
})
const { t } = useI18n()

function copyId() {
  const id = props.peer.peer_id || props.peer.node_id
  if (id) navigator.clipboard?.writeText(id).catch(() => {})
}
</script>

<template>
  <!--
    List mode previously used a plain <table>: the IP/multiaddr column had no width cap (a
    full multiaddr is ~80+ chars) and the Telemetry column crammed 7 pill badges into
    whatever narrow width table layout left it, wrapping across many lines and blowing up
    row height -- both together pushed the row well past the viewport and squeezed/clipped
    the right-hand columns. Fixed by dropping table semantics for a two-tier row: a main
    row with only identity fields (each independently truncated with a hover tooltip via
    `title`, using flex-1 min-w-0 so they share space instead of one forcing the row wide),
    and telemetry pills demoted to their own wrapping sub-row underneath.
  -->
  <div class="flex flex-col gap-2 px-4 py-3 border-b border-white/5 hover:bg-white/5 transition-colors min-w-0 w-full max-w-full box-border">
    <!-- Main row: rank, status, identity, GPU model, copy action -->
    <div class="flex items-center gap-3 min-w-0">
      <span class="w-6 shrink-0 text-center">
        <span v-if="medal" class="text-base">{{ medal }}</span>
        <span v-else class="text-ink-faint text-xs font-semibold">#{{ rank }}</span>
      </span>
      <span
        class="h-2 w-2 rounded-full shrink-0"
        :class="(parseGpuInfo(peer).status || 'idle') === 'idle' ? 'bg-emerald-400' : 'bg-amber-400'"
      />
      <div class="h-7 w-7 shrink-0 rounded-lg bg-brand flex items-center justify-center text-slate-950 text-xs font-bold">
        {{ (peer.peer_id || peer.node_id || 'ND').substring(0, 2).toUpperCase() }}
      </div>

      <div class="min-w-0 flex-1 flex items-center gap-1.5">
        <span
          class="font-mono text-xs font-semibold text-brand-light truncate"
          :title="peer.peer_id || peer.node_id || '-'"
        >{{ peer.peer_id || peer.node_id || '-' }}</span>
        <span v-if="isLocal" class="pill pill-blue text-[0.6rem] py-0 shrink-0">{{ t('local_badge') }}</span>
      </div>

      <div
        class="min-w-0 flex-1 font-mono text-xs text-ink-faint truncate"
        :title="peer.ip_address || peer.addr || '-'"
      >{{ peer.ip_address || peer.addr || '-' }}</div>

      <div class="min-w-0 shrink-0 max-w-[40%] text-xs font-semibold text-ink truncate" :title="parseGpuInfo(peer).summary || ''">
        <span v-if="!parseGpuInfo(peer).summary" class="pill pill-red text-[0.65rem]">{{ t('no_gpu') }}</span>
        <span v-else>{{ parseGpuInfo(peer).summary }}</span>
      </div>

      <!-- Optional per-page extra (e.g. hub status pill, rank medal/score) -- kept out of the
           shared component's own fields so callers don't need a matching data shape. -->
      <div class="shrink-0"><slot name="extra" /></div>

      <button
        class="shrink-0 rounded-md px-2 py-1 text-ink-faint hover:bg-white/10 hover:text-ink transition-colors"
        :title="t('copy_id')"
        @click="copyId"
      >📋</button>
    </div>

    <!-- Sub-row: telemetry pills, indented to align under the identity column, free to wrap -->
    <div class="pl-9 min-w-0 flex flex-wrap items-center gap-2">
      <TelemetryBadges :info="parseGpuInfo(peer)" />
      <slot name="sub-extra" />
    </div>
  </div>
</template>
