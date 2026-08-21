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

  <div class="grid grid-cols-1 gap-5 p-8 lg:grid-cols-[2fr_1fr]">
    <div class="card">
      <h2 class="mb-3 text-sm font-semibold">Edit config.json</h2>
      <textarea
        v-model="configText"
        spellcheck="false"
        class="code-panel h-[450px] w-full resize-y !text-lime-400"
      ></textarea>
      <div class="mt-4">
        <button class="btn btn-warning" @click="onSave">💾 Save &amp; Backup</button>
      </div>
    </div>

    <div class="card">
      <h2 class="mb-3 text-sm font-semibold">Restore Backups</h2>
      <ul class="max-h-[400px] list-none overflow-y-auto">
        <li v-if="!backups.length" class="p-4 text-ink-muted">No backups</li>
        <li
          v-for="b in backups"
          :key="b"
          class="flex items-center justify-between border-b border-border py-2.5 hover:bg-white/[0.03]"
        >
          <span class="text-sm">{{ backupLabel(b) }}</span>
          <button class="btn btn-ghost btn-sm" @click="onRestore(b)">Restore</button>
        </li>
      </ul>
    </div>
  </div>
</template>
