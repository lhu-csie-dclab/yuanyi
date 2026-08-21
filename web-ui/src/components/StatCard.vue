<script setup>
defineProps({
  label: { type: String, required: true },
  value: { type: [String, Number], required: true },
  accent: { type: Boolean, default: false },
  bare: { type: Boolean, default: false },
})
</script>

<template>
  <div :class="bare ? 'flex flex-col' : 'card flex flex-col justify-between !p-5 hover:translate-y-[-2px] transition-transform duration-200'">
    <div class="text-[0.72rem] font-semibold tracking-wider text-slate-500 uppercase">
      {{ label }}
    </div>
    <div
      class="mt-2 text-2xl sm:text-3xl font-extrabold leading-tight tabular-nums tracking-tight"
      :class="accent ? 'text-brand' : 'text-slate-800'"
    >
      {{ value }}
    </div>
  </div>
</template>

