<script setup>
defineProps({
  currentPage: { type: Number, required: true },
  totalPages:  { type: Number, required: true },
  totalItems:  { type: Number, required: true },
  pageSize:    { type: Number, default: 25 },
})

const emit = defineEmits(['update:currentPage'])
</script>

<template>
  <div v-if="totalItems > pageSize" class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 border-t border-white/5 bg-white/[0.03]">
    <div class="text-xs text-ink-faint">
      <span class="font-medium text-ink-muted">
        {{ (currentPage - 1) * pageSize + 1 }}–{{ Math.min(currentPage * pageSize, totalItems) }}
      </span>
      <span class="mx-1 text-ink-faint">/</span>
      <span>共 {{ totalItems }} 筆</span>
      <span class="ml-2 text-ink-faint">({{ totalPages }} 頁)</span>
    </div>

    <div class="flex items-center gap-1.5">
      <button
        class="btn btn-ghost btn-sm text-xs font-semibold px-2.5 py-1 disabled:opacity-40 disabled:cursor-not-allowed"
        :disabled="currentPage <= 1"
        @click="emit('update:currentPage', currentPage - 1)"
      >
        ‹ 上一頁
      </button>

      <div class="flex items-center gap-1">
        <button
          v-for="p in totalPages"
          :key="p"
          class="h-7 min-w-[28px] px-1.5 rounded-lg text-xs font-semibold transition-colors"
          :class="p === currentPage ? 'bg-brand text-white shadow-xs' : 'text-ink-muted hover:bg-white/10'"
          @click="emit('update:currentPage', p)"
        >
          {{ p }}
        </button>
      </div>

      <button
        class="btn btn-ghost btn-sm text-xs font-semibold px-2.5 py-1 disabled:opacity-40 disabled:cursor-not-allowed"
        :disabled="currentPage >= totalPages"
        @click="emit('update:currentPage', currentPage + 1)"
      >
        下一頁 ›
      </button>
    </div>
  </div>
</template>
