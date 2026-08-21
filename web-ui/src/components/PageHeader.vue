<script setup>
import { computed } from 'vue'
import { useNodeInfo } from '../composables/useNodeInfo.js'

defineProps({
  title: { type: String, required: true },
  badge: { type: String, default: '' },
})

const nodeInfo = useNodeInfo()

const avatarLetter = computed(() =>
  nodeInfo.localNodeId ? nodeInfo.localNodeId.substring(0, 1).toUpperCase() : 'N'
)
const displayName = computed(() =>
  nodeInfo.localNodeId ? 'Node ' + nodeInfo.localNodeId.substring(0, 8) : 'Node Cluster'
)
</script>

<template>
  <div class="sticky top-14 lg:top-0 z-20 flex items-center justify-between border-b border-border bg-white/90 px-4 py-3.5 sm:px-6 sm:py-4 lg:px-8 lg:py-4.5 backdrop-blur-md">
    <div class="flex items-center gap-3">
      <h1 class="text-base sm:text-lg lg:text-xl font-bold tracking-tight text-slate-900">{{ title }}</h1>
      <span v-if="badge" class="rounded-full bg-blue-50 px-2.5 py-1 text-xs font-semibold text-brand border border-blue-100 shadow-xs">
        {{ badge }}
      </span>
    </div>

    <!-- Top Right Profile Capsule (Confiss Style) -->
    <div class="hidden sm:flex items-center gap-2.5 rounded-full border border-slate-200 bg-slate-50/80 px-3 py-1.5 shadow-xs">
      <div class="flex h-6 w-6 items-center justify-center rounded-full bg-blue-600 text-xs font-bold text-white shadow-xs">
        {{ avatarLetter }}
      </div>
      <span class="text-xs font-semibold text-slate-700 font-mono">{{ displayName }}</span>
      <span class="h-2 w-2 rounded-full" :class="nodeInfo.loaded ? 'bg-emerald-500' : 'bg-amber-500 animate-pulse'" />
    </div>
  </div>
</template>


