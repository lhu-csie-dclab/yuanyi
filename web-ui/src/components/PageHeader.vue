<script setup>
defineProps({
  title: { type: String, required: true },
  badge: { type: String, default: '' },
})
</script>

<template>
  <div class="sticky top-14 lg:top-0 z-20 flex items-center justify-between border-b border-border bg-surface-raised/90 px-4 py-3.5 sm:px-6 sm:py-4 lg:px-8 lg:py-5 backdrop-blur-md">
    <h1 class="text-base sm:text-lg lg:text-xl font-bold tracking-tight text-white">{{ title }}</h1>
    <span v-if="badge" class="rounded-full bg-brand/15 px-2.5 py-1 sm:px-3.5 sm:py-1.5 text-[0.7rem] sm:text-xs font-semibold text-brand-light border border-brand/20">
      {{ badge }}
    </span>
  </div>
</template>

