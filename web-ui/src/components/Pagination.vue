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
  <div v-if="totalItems > pageSize" class="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5 border-t border-slate-100 bg-slate-50/50">
    <div class="text-xs text-slate-500">
      <span class="font-medium text-slate-700">
        {{ (currentPage - 1) * pageSize + 1 }}–{{ Math.min(currentPage * pageSize, totalItems) }}
      </span>
      <span class="mx-1 text-slate-400">/</span>
      <span>共 {{ totalItems }} 筆</span>
      <span class="ml-2 text-slate-400">({{ totalPages }} 頁)</span>
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
          :class="p === currentPage ? 'bg-blue-600 text-white shadow-xs' : 'text-slate-600 hover:bg-slate-200/70'"
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
