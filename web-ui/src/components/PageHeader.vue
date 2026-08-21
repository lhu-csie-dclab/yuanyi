<script setup>
defineProps({
  title: { type: String, required: true },
  badge: { type: String, default: '' },
})
</script>

<template>
  <div class="sticky top-0 lg:top-0 z-20 flex items-center justify-between border-b border-slate-200 bg-white/95 px-5 py-3.5 backdrop-blur-md">
    <h1 class="text-base font-bold text-slate-900">{{ title }}</h1>
    <span v-if="badge" class="pill pill-blue">{{ badge }}</span>
  </div>
</template>
