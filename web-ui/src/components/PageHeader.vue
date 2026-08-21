<script setup>
defineProps({
  title: { type: String, required: true },
  badge: { type: String, default: '' },
})
</script>

<template>
  <div class="sticky top-0 z-30 flex items-center justify-between border-b border-border bg-surface-raised/90 px-8 py-5 backdrop-blur-md">
    <h1 class="text-xl font-bold">{{ title }}</h1>
    <span v-if="badge" class="rounded-full bg-brand/15 px-3.5 py-1.5 text-xs font-semibold text-brand-light">
      {{ badge }}
    </span>
  </div>
</template>
