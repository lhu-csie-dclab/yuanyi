<script setup>
// Raw telemetry readouts, not status signals -- so unlike StatusPill these
// stay a single neutral tone (icon + muted text) rather than each metric
// inventing its own decorative hue. The one exception (GPU temp past a
// threshold) is a real signal and gets the reserved warning/critical color.
const props = defineProps({
  info: { type: Object, required: true },
})

function tempVariant(temp) {
  if (temp >= 85) return 'text-critical'
  if (temp >= 75) return 'text-warning'
  return 'text-ink-muted'
}
</script>

<template>
  <div class="flex flex-wrap gap-1.5">
    <span class="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs text-slate-600 border border-slate-200/60 font-medium">
      Req {{ info.active_requests || 0 }}
    </span>
    <span class="inline-flex items-center gap-1 rounded-lg bg-blue-50 px-2 py-1 text-xs text-blue-700 border border-blue-100 font-semibold">
      Gen {{ (info.gen_speed || 0).toFixed(1) }} t/s
    </span>
    <span class="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs text-slate-600 border border-slate-200/60 font-medium">
      TTFT {{ (info.avg_ttft || 0).toFixed(2) }}s
    </span>
    <span
      v-if="info.gpu_temp"
      class="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs font-semibold border border-slate-200/60"
      :class="tempVariant(info.gpu_temp)"
    >
      🌡 {{ info.gpu_temp }}°C
    </span>
    <span v-if="info.gpu_util !== undefined" class="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs text-slate-600 border border-slate-200/60 font-medium">
      ⚡ {{ info.gpu_util }}%
    </span>
    <span v-if="info.vram_used && info.vram_total" class="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs text-slate-600 border border-slate-200/60 font-medium">
      💾 {{ info.vram_used }}/{{ info.vram_total }}MB
    </span>
    <span v-if="info.power_draw" class="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs text-slate-600 border border-slate-200/60 font-medium">
      🔌 {{ info.power_draw.toFixed(0) }}{{ info.power_limit ? '/' + info.power_limit.toFixed(0) : '' }}W
    </span>
    <span v-if="info.fan_speed" class="inline-flex items-center gap-1 rounded-lg bg-slate-100 px-2 py-1 text-xs text-slate-600 border border-slate-200/60 font-medium">
      🌀 {{ info.fan_speed }}%
    </span>
    <span v-if="info.driver_version" class="inline-flex items-center gap-1 rounded-lg bg-slate-50 px-2 py-1 text-xs text-slate-400 border border-slate-200/40">
      v{{ info.driver_version }}
    </span>
  </div>
</template>
