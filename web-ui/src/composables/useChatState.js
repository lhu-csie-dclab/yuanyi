import { ref, computed, watch } from 'vue'
import { useNodeInfo } from './useNodeInfo.js'

// Chat session/config state, module-level singleton -- mirrors useNodeInfo.js's pattern.
// This used to live inside ChatView's own <script setup>, which Vue Router destroys on
// navigation: an in-flight streaming send() kept mutating a component instance nothing was
// bound to anymore, and saveSessions() only ever ran once, at the very end of a full stream --
// so switching away mid-reply and coming back loaded a stale localStorage snapshot with the
// reply missing. Keeping the state here instead means navigating away and back re-mounts
// ChatView against the SAME live objects, including a still-in-progress stream.
const STORAGE_KEY_SESSIONS = 'chat_sessions_v1'
const STORAGE_KEY_CONFIG = 'chat_config_v1'
const STORAGE_KEY_ENDPOINT_OVERRIDDEN = 'chat_endpoint_overridden_v1'

const sessions = ref([])
const activeId = ref(null)
const streaming = ref(false)
const error = ref('')
const cfg = ref({
  endpoint: 'http://localhost:50006/v1/chat/completions',
  model: '',
  systemPrompt: '',
  temperature: 0.7,
  maxTokens: 2048,
  apiKey: '',
})

const activeSession = computed(() => sessions.value.find((s) => s.id === activeId.value) || null)

function saveSessions() {
  localStorage.setItem(STORAGE_KEY_SESSIONS, JSON.stringify(sessions.value))
}
function loadSessions() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_SESSIONS)
    if (raw) sessions.value = JSON.parse(raw)
  } catch {}
  if (!sessions.value.length) newSession()
  else if (!sessions.value.some((s) => s.id === activeId.value)) activeId.value = sessions.value[0].id
}
function saveCfg() {
  localStorage.setItem(STORAGE_KEY_CONFIG, JSON.stringify(cfg.value))
}
function loadCfg() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_CONFIG)
    if (raw) Object.assign(cfg.value, JSON.parse(raw))
  } catch {}
}

let _sid = Date.now()
function newSession() {
  const id = String(++_sid)
  sessions.value.unshift({ id, title: '新對話', model: cfg.value.model, messages: [] })
  activeId.value = id
  saveSessions()
}
function deleteSession(id) {
  sessions.value = sessions.value.filter((s) => s.id !== id)
  if (activeId.value === id) {
    activeId.value = sessions.value[0]?.id || null
    if (!sessions.value.length) newSession()
  }
  saveSessions()
}
function selectSession(id) {
  activeId.value = id
}
function renameSession(id, title) {
  const s = sessions.value.find((s) => s.id === id)
  if (s) {
    s.title = title
    saveSessions()
  }
}

async function send(text) {
  const trimmed = (text ?? '').trim()
  const session = activeSession.value
  if (!trimmed || streaming.value || !session) return
  error.value = ''

  session.messages.push({ role: 'user', content: trimmed })
  if (session.messages.length === 1) {
    session.title = trimmed.substring(0, 28) + (trimmed.length > 28 ? '…' : '')
  }
  saveSessions()

  const assistantMsg = { role: 'assistant', content: '', loading: true }
  session.messages.push(assistantMsg)
  streaming.value = true

  const apiMessages = []
  if (cfg.value.systemPrompt) {
    apiMessages.push({ role: 'system', content: cfg.value.systemPrompt })
  }
  apiMessages.push(
    ...session.messages.filter((m) => !m.loading).map((m) => ({ role: m.role, content: m.content }))
  )

  const payload = {
    model: cfg.value.model || undefined,
    messages: apiMessages,
    temperature: cfg.value.temperature,
    max_tokens: cfg.value.maxTokens,
    stream: true,
  }

  try {
    const res = await fetch(cfg.value.endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(cfg.value.apiKey ? { Authorization: `Bearer ${cfg.value.apiKey}` } : {}),
      },
      body: JSON.stringify(payload),
    })

    if (!res.ok) {
      const msg = await res.text()
      throw new Error(`HTTP ${res.status}: ${msg}`)
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    let sinceSave = 0

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue
        const data = line.slice(6).trim()
        if (data === '[DONE]') continue
        try {
          const chunk = JSON.parse(data)
          const delta = chunk.choices?.[0]?.delta?.content
          if (delta) {
            assistantMsg.content += delta
            assistantMsg.loading = false
            // Persist periodically while streaming, not just once at the end -- the
            // module-singleton above already survives in-SPA navigation on its own, but this
            // also covers an actual full page reload/tab close mid-stream.
            if (++sinceSave >= 20) {
              sinceSave = 0
              saveSessions()
            }
          }
        } catch {}
      }
    }
  } catch (e) {
    assistantMsg.content = ''
    error.value = e.message
  }

  assistantMsg.loading = false
  streaming.value = false
  saveSessions()
}

function clearSession() {
  const session = activeSession.value
  if (!session) return
  session.messages = []
  saveSessions()
}

let initialized = false

export function useChatState() {
  if (!initialized) {
    initialized = true
    loadCfg()
    loadSessions()

    const hasOverride = localStorage.getItem(STORAGE_KEY_ENDPOINT_OVERRIDDEN)
    if (!hasOverride) {
      const nodeInfo = useNodeInfo()
      const stopWatch = watch(
        () => nodeInfo.loaded,
        (loaded) => {
          if (!loaded) return
          stopWatch()
          const host = window.location.hostname || '127.0.0.1'
          cfg.value.endpoint = `http://${host}:${nodeInfo.proxyPort}/v1/chat/completions`
          if (nodeInfo.modelName) cfg.value.model = nodeInfo.modelName
          saveCfg()
        },
        { immediate: true }
      )
    }

    watch(cfg, saveCfg, { deep: true })
  }

  return {
    sessions,
    activeId,
    streaming,
    error,
    cfg,
    activeSession,
    STORAGE_KEY_ENDPOINT_OVERRIDDEN,
    newSession,
    deleteSession,
    selectSession,
    renameSession,
    send,
    clearSession,
  }
}
