<script setup>
import { fmtNum } from '../api.js'

defineProps({ info: { type: Object, required: true } })

function tempClass(t) {
  if (t >= 85) return 'text-rose-400'
  if (t >= 75) return 'text-amber-400'
  return 'text-ink-faint'
}
</script>

<template>
  <div class="flex flex-wrap gap-1.5">
    <!-- Cumulative work done. Shown first because the three live gauges below it read 0
         whenever a node is not mid-generation, which is almost always when you happen to
         look -- so on their own they made every node appear to have done nothing. Absent
         (omitempty on the Go side) rather than 0 for a node that has served nothing, so
         v-if hides it instead of printing a meaningless "0 tok". -->
    <span v-if="info.total_tokens" class="pill pill-cyan" title="Total tokens generated">
      Σ {{ fmtNum(info.total_tokens) }} tok
    </span>
    <span class="pill pill-blue">{{ info.active_requests || 0 }} req</span>
    <span class="pill pill-blue">{{ (info.gen_speed || 0).toFixed(1) }} t/s</span>
    <span class="pill pill-blue">TTFT {{ (info.avg_ttft || 0).toFixed(2) }}s</span>
    <span v-if="info.gpu_temp" class="pill" :class="info.gpu_temp >= 75 ? 'pill-amber' : 'pill-blue'">
      {{ info.gpu_temp }}°C
    </span>
    <span v-if="info.gpu_util !== undefined" class="pill pill-blue">{{ info.gpu_util }}%</span>
    <span v-if="info.vram_used && info.vram_total" class="pill pill-blue">{{ info.vram_used }}/{{ info.vram_total }}M</span>
    <span v-if="info.power_draw" class="pill pill-blue">{{ info.power_draw.toFixed(0) }}W</span>
  </div>
</template>
