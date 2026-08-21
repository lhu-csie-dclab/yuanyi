<script setup>
defineProps({
  label: { type: String, required: true },
  value: { type: [String, Number], required: true },
  accent: { type: Boolean, default: false },
  bare: { type: Boolean, default: false },
})
</script>

<template>
  <div :class="bare ? 'flex flex-col' : 'card flex flex-col !p-5'">
    <div
      class="text-2xl font-bold leading-tight tabular-nums"
      :class="accent ? 'text-brand-light' : 'text-ink'"
    >
      {{ value }}
    </div>
    <div class="mt-2 text-[0.68rem] font-medium uppercase tracking-wider text-ink-muted">
      {{ label }}
    </div>
  </div>
</template>
