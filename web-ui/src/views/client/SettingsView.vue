<script setup>
import { ref, onMounted } from 'vue'
import { getConfig, saveConfig, getBackups, restoreBackup } from '../../api.js'
import { useToast } from '../../composables/useToast.js'
import PageHeader from '../../components/PageHeader.vue'

const toast = useToast()
const configText = ref('')
const backups = ref([])

async function loadConfig() {
  try {
    configText.value = await getConfig()
  } catch {
    toast.error('Failed to load config')
  }
}

async function loadBackups() {
  try {
    backups.value = (await getBackups()) || []
  } catch {
    backups.value = []
  }
}

async function onSave() {
  try {
    const d = await saveConfig(configText.value)
    toast.success(d.message || 'Saved')
    loadBackups()
  } catch (e) {
    toast.error(e.message || 'Save failed')
  }
}

async function onRestore(filename) {
  if (!confirm('Restore this backup? Current config will be saved first.')) return
  try {
    const d = await restoreBackup(filename)
    toast.success(d.message || 'Restored')
    loadConfig()
    loadBackups()
  } catch (e) {
    toast.error(e.message || 'Restore failed')
  }
}

function backupLabel(filename) {
  return filename.replace('config_', '').replace('.json', '')
}

onMounted(() => {
  loadConfig()
  loadBackups()
})
</script>

<template>
  <PageHeader title="Config & Backups" />

  <div class="grid grid-cols-1 gap-4 sm:gap-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0 lg:grid-cols-[2fr_1fr]">
    <div class="card min-w-0">
      <h2 class="mb-3 text-xs sm:text-sm font-semibold text-white flex items-center gap-2">
        <span>⚙️</span> Edit config.json
      </h2>
      <textarea
        v-model="configText"
        spellcheck="false"
        class="code-panel h-[320px] sm:h-[450px] w-full resize-y text-xs sm:text-sm !text-lime-400 font-mono"
      ></textarea>
      <div class="mt-4">
        <button class="btn btn-warning" @click="onSave">💾 Save &amp; Backup</button>
      </div>
    </div>

    <div class="card min-w-0">
      <h2 class="mb-3 text-xs sm:text-sm font-semibold text-white flex items-center gap-2">
        <span>📦</span> Restore Backups
      </h2>
      <ul class="max-h-[300px] sm:max-h-[450px] list-none overflow-y-auto divide-y divide-border">
        <li v-if="!backups.length" class="p-4 text-center text-xs sm:text-sm text-ink-muted">No backups</li>
        <li
          v-for="b in backups"
          :key="b"
          class="flex items-center justify-between gap-2 py-2.5 px-1 hover:bg-white/[0.03] transition-colors"
        >
          <span class="text-xs sm:text-sm font-mono text-ink truncate">{{ backupLabel(b) }}</span>
          <button class="btn btn-ghost btn-sm shrink-0" @click="onRestore(b)">Restore</button>
        </li>
      </ul>
    </div>
  </div>
</template>
