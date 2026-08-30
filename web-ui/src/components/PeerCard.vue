<script setup>
import { parseGpuInfo } from '../api.js'
import { useI18n } from '../composables/useI18n.js'
import TelemetryBadges from './TelemetryBadges.vue'

const props = defineProps({
  peer: { type: Object, required: true },
  isLocal: { type: Boolean, default: false },
  medal: { type: String, default: '' }, // '🥇' | '🥈' | '🥉' | '' -- only set on the Top Ranking page
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
    min-w-0 is load-bearing: without it, a flex/grid child defaults to min-width:auto, which
    lets its content (the long monospace node ID) force the card wider than the grid track --
    that's what was cascading into page-level horizontal overflow on narrow viewports.
  -->
  <div class="min-w-0 w-full box-border rounded-xl border border-border bg-white/[0.03] p-4 flex flex-col gap-3 transition-all duration-150 hover:border-brand/40 hover:-translate-y-px">
    <div class="flex items-start justify-between gap-2 min-w-0">
      <div class="flex items-center gap-2 min-w-0">
        <span v-if="medal" class="text-lg shrink-0">{{ medal }}</span>
        <span v-else-if="rank" class="text-ink-faint text-xs font-semibold shrink-0">#{{ rank }}</span>
        <div class="min-w-0">
          <!-- Single-line truncate, not line-clamp: this "title" is a fixed-format hash ID,
               not natural-language text, so wrapping it across lines would just fragment it
               mid-hash. Truncate-with-ellipsis is the correct treatment for this content type. -->
          <div class="font-mono text-xs font-semibold text-brand-light truncate">
            {{ (peer.peer_id || peer.node_id || '-').substring(0, 20) }}…
          </div>
          <div class="flex items-center gap-1 mt-0.5 flex-wrap">
            <span v-if="isLocal" class="pill pill-blue text-[0.6rem] py-0">{{ t('local_badge') }}</span>
            <span v-if="!parseGpuInfo(peer).summary" class="pill pill-red text-[0.6rem] py-0">{{ t('relay_badge') }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="text-xs font-semibold text-ink min-w-0 truncate">
      <span v-if="!parseGpuInfo(peer).summary" class="text-ink-faint">{{ t('no_gpu') }}</span>
      <span v-else>{{ parseGpuInfo(peer).summary }}</span>
    </div>

    <TelemetryBadges :info="parseGpuInfo(peer)" />

    <!-- Action row: grid-cols-2 so buttons never overlap/overflow on a narrow card, unlike a
         flex row that can squeeze or wrap unpredictably at small widths. -->
    <div class="grid grid-cols-2 gap-2 pt-2 border-t border-white/5">
      <div class="text-[0.68rem] text-ink-faint font-mono truncate self-center">{{ peer.ip_address || peer.addr || '-' }}</div>
      <button
        class="justify-self-end rounded-md px-2 py-1 text-[0.68rem] text-ink-faint hover:bg-white/10 hover:text-ink transition-colors"
        :title="t('copy_id')"
        @click="copyId"
      >📋 {{ t('copy_id') }}</button>
    </div>
  </div>
</template>
