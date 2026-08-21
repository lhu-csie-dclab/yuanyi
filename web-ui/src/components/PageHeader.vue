<script setup>
import { computed } from 'vue'

defineProps({
  title: { type: String, required: true },
  badge: { type: String, default: '' },
})

const currentDate = new Date().toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
</script>

<template>
  <div class="flex flex-wrap items-center justify-between gap-3 px-4 py-4 sm:px-6 sm:py-5 border-b border-slate-200/80 bg-white/60 backdrop-blur-xs">
    <!-- Left: Action Pill Button -->
    <div class="flex items-center gap-3">
      <button class="inline-flex items-center gap-1.5 rounded-full bg-[#1c5b88] hover:bg-[#154668] text-white px-4 py-1.5 text-xs font-semibold shadow-xs transition-colors cursor-pointer">
        <span>+</span> Add Node / Swarm
      </button>
      <span v-if="badge" class="rounded-full bg-blue-50 px-3 py-1 text-xs font-semibold text-[#1c5b88] border border-blue-200/60 shadow-2xs">
        {{ badge }}
      </span>
    </div>

    <!-- Center: Section Title & Date Indicator (Reference Image Style) -->
    <div class="text-xs sm:text-sm font-bold tracking-wider uppercase text-slate-600 flex items-center gap-2">
      <span>{{ title }}</span>
      <span class="text-slate-300">•</span>
      <span class="text-slate-400 font-mono text-xs">{{ currentDate }}</span>
    </div>

    <!-- Right: Status Capsule -->
    <div class="flex items-center gap-2">
      <span class="inline-flex items-center gap-1.5 rounded-lg bg-white border border-slate-200 px-2.5 py-1 text-xs font-medium text-slate-600 shadow-2xs">
        <span class="h-2 w-2 rounded-full bg-emerald-500" />
        Cluster Online
      </span>
    </div>
  </div>
</template>



