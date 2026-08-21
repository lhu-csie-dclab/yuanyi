<script setup>
defineProps({ info: { type: Object, required: true } })

function tempClass(t) {
  if (t >= 85) return 'text-rose-600'
  if (t >= 75) return 'text-amber-500'
  return 'text-slate-500'
}
</script>

<template>
  <div class="flex flex-wrap gap-1.5">
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
