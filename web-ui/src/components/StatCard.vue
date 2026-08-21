<script setup>
defineProps({
  label: { type: String, required: true },
  value: { type: [String, Number], required: true },
  icon:  { type: String, default: '' },
  accent: { type: Boolean, default: false },
  bare:   { type: Boolean, default: false },
  color:  { type: String, default: '' }, // bg-* for icon bg
})
</script>

<template>
  <!-- ── bare: inside a parent card ── -->
  <div v-if="bare" class="flex flex-col gap-0.5">
    <span class="metric-label">{{ label }}</span>
    <span class="text-xl font-bold tabular-nums" :class="accent ? 'text-blue-600' : 'text-slate-900'">{{ value }}</span>
  </div>

  <!-- ── standalone card ── -->
  <div v-else class="metric-card">
    <div v-if="icon" class="metric-icon" :class="color || 'bg-slate-100'">{{ icon }}</div>
    <span class="metric-label">{{ label }}</span>
    <span class="metric-value" :class="accent ? 'text-blue-600' : 'text-slate-900'">{{ value }}</span>
  </div>
</template>
