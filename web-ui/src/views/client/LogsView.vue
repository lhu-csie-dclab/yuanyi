<script setup>
import { ref, nextTick } from 'vue'
import { getLogs } from '../../api.js'
import { usePolling } from '../../composables/usePolling.js'
import PageHeader from '../../components/PageHeader.vue'

const sysLogs = ref([])
const vllmLogs = ref([])
const dockerLogs = ref([])
const autoScroll = ref(true)

const sysBox = ref(null)
const vllmBox = ref(null)
const dockerBox = ref(null)

async function refresh() {
  try {
    const data = await getLogs()
    sysLogs.value = data.sys_logs || []
    vllmLogs.value = data.vllm_logs || []
    dockerLogs.value = data.docker_logs || []
    if (autoScroll.value) {
      await nextTick()
      for (const el of [sysBox.value, vllmBox.value, dockerBox.value]) {
        if (el) el.scrollTop = el.scrollHeight
      }
    }
  } catch {
    /* transient poll failure, next tick retries */
  }
}

usePolling(refresh, 2000)
</script>

<template>
  <PageHeader title="Real-time Logs" />

  <div class="space-y-4 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <div class="flex items-center justify-between">
      <label class="flex cursor-pointer items-center gap-2 text-xs sm:text-sm text-ink-muted">
        <input v-model="autoScroll" type="checkbox" class="accent-brand h-4 w-4 rounded" />
        Auto-scroll
      </label>
      <button class="btn btn-ghost btn-sm" @click="refresh">🔄 Refresh</button>
    </div>

    <div class="card">
      <h2 class="mb-2 text-xs sm:text-sm font-semibold text-white flex items-center gap-2">
        <span>🖥️</span> System &amp; P2P Logs
      </h2>
      <div ref="sysBox" class="code-panel h-64 sm:h-80 overflow-y-auto overflow-x-auto whitespace-pre-wrap break-all text-xs sm:text-sm !text-lime-400">
        <template v-if="sysLogs.length"><div v-for="(l, i) in sysLogs" :key="i" v-html="l" /></template>
        <span v-else class="text-ink-muted">No logs yet</span>
      </div>
    </div>
    <div class="card">
      <h2 class="mb-2 text-xs sm:text-sm font-semibold text-white flex items-center gap-2">
        <span>⚡</span> vLLM Model Logs
      </h2>
      <div ref="vllmBox" class="code-panel h-64 sm:h-80 overflow-y-auto overflow-x-auto whitespace-pre-wrap break-all text-xs sm:text-sm !text-lime-400">
        <template v-if="vllmLogs.length"><div v-for="(l, i) in vllmLogs" :key="i" v-html="l" /></template>
        <span v-else class="text-ink-muted">No logs yet</span>
      </div>
    </div>
    <div class="card">
      <h2 class="mb-2 text-xs sm:text-sm font-semibold text-white flex items-center gap-2">
        <span>🐳</span> Docker Logs
      </h2>
      <div ref="dockerBox" class="code-panel h-64 sm:h-80 overflow-y-auto overflow-x-auto whitespace-pre-wrap break-all text-xs sm:text-sm !text-lime-400">
        <template v-if="dockerLogs.length"><div v-for="(l, i) in dockerLogs" :key="i" v-html="l" /></template>
        <span v-else class="text-ink-muted">No logs yet</span>
      </div>
    </div>
  </div>
</template>
