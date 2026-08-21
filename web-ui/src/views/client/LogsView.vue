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

  <div class="space-y-5 p-8">
    <div class="flex items-center justify-between">
      <label class="flex cursor-pointer items-center gap-2 text-sm text-ink-muted">
        <input v-model="autoScroll" type="checkbox" class="accent-brand" />
        Auto-scroll
      </label>
      <button class="btn btn-ghost btn-sm" @click="refresh">🔄 Refresh</button>
    </div>

    <div class="card">
      <h2 class="mb-2 text-sm font-semibold">System & P2P Logs</h2>
      <div ref="sysBox" class="code-panel h-80 overflow-y-auto whitespace-pre-wrap break-all !text-lime-400">
        <template v-if="sysLogs.length"><div v-for="(l, i) in sysLogs" :key="i" v-html="l" /></template>
        <span v-else class="text-ink-muted">No logs yet</span>
      </div>
    </div>
    <div class="card">
      <h2 class="mb-2 text-sm font-semibold">vLLM Model Logs</h2>
      <div ref="vllmBox" class="code-panel h-80 overflow-y-auto whitespace-pre-wrap break-all !text-lime-400">
        <template v-if="vllmLogs.length"><div v-for="(l, i) in vllmLogs" :key="i" v-html="l" /></template>
        <span v-else class="text-ink-muted">No logs yet</span>
      </div>
    </div>
    <div class="card">
      <h2 class="mb-2 text-sm font-semibold">Docker Logs</h2>
      <div ref="dockerBox" class="code-panel h-80 overflow-y-auto whitespace-pre-wrap break-all !text-lime-400">
        <template v-if="dockerLogs.length"><div v-for="(l, i) in dockerLogs" :key="i" v-html="l" /></template>
        <span v-else class="text-ink-muted">No logs yet</span>
      </div>
    </div>
  </div>
</template>
