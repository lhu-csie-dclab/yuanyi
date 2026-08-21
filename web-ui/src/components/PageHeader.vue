<script setup>
defineProps({
  title: { type: String, required: true },
  badge: { type: String, default: '' },
})
</script>

<template>
  <div class="sticky top-14 lg:top-0 z-20 flex items-center justify-between border-b border-slate-200 bg-white/95 px-4 py-3.5 sm:px-6 sm:py-4 lg:px-8 lg:py-4.5 backdrop-blur-md">
    <h1 class="text-base sm:text-lg lg:text-xl font-bold tracking-tight text-slate-900">{{ title }}</h1>
    <span v-if="badge" class="rounded-full bg-blue-50 px-3 py-1 text-xs font-semibold text-blue-700 border border-blue-200 shadow-2xs">
      {{ badge }}
    </span>
  </div>
</template>




