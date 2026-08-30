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

// The initial default endpoint host must come from window.location.hostname, available
// synchronously at module load, NOT a hardcoded 'localhost'. A hardcoded default combined
// with the async /api/node_info-based correction below created a race: on a slower
// connection (e.g. a phone over WiFi), a message sent before that fetch resolves went out
// to 'localhost:50006' -- the sending DEVICE's own loopback, not the dashboard's host --
// and failed with "Failed to fetch". Reading the real host immediately removes the window
// entirely; only the port (if proxy_port was customized away from the 50006 default) still
// needs the async correction.
function defaultEndpoint() {
  const host = (typeof window !== 'undefined' && window.location.hostname) || '127.0.0.1'
  return `http://${host}:50006/v1/chat/completions`
}

const sessions = ref([])
const activeId = ref(null)
const streaming = ref(false)
const error = ref('')
const cfg = ref({
  endpoint: defaultEndpoint(),
  model: '',
  systemPrompt: '',
  temperature: 0.7,
  maxTokens: 2048,
  apiKey: '',
})

const activeSession = computed(() => sessions.value.find((s) => s.id === activeId.value) || null)

function saveSessions() {
  // Attached images are stored inline as base64 data URLs (see send()'s images param), which
  // can push a session well past localStorage's ~5-10MB per-origin quota after a handful of
  // photos. setItem throws QuotaExceededError in that case -- catch it so a save failure
  // (this call runs mid-stream, inside send()) surfaces as a lost-persistence warning rather
  // than an uncaught exception breaking the send/receive flow itself.
  try {
    localStorage.setItem(STORAGE_KEY_SESSIONS, JSON.stringify(sessions.value))
  } catch (e) {
    error.value = `Failed to save chat history locally (${e.name || 'error'}) -- it may be too large for browser storage.`
  }
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

  // A saved endpoint whose host no longer matches how this page is currently being
  // accessed is stale, not intentional -- e.g. a value auto-corrected on a previous visit
  // from a different network path (localStorage is per-origin, but the auto-corrected
  // *content* saved into it isn't tied to origin the way the storage itself is), or a
  // leftover 'localhost' saved by the race this function's caller now fixes on first
  // render. Only reset it when the user never explicitly chose a custom endpoint
  // (STORAGE_KEY_ENDPOINT_OVERRIDDEN unset) -- an explicit override always wins.
  if (!localStorage.getItem(STORAGE_KEY_ENDPOINT_OVERRIDDEN)) {
    try {
      const savedHost = new URL(cfg.value.endpoint).hostname
      if (savedHost !== window.location.hostname) {
        cfg.value.endpoint = defaultEndpoint()
      }
    } catch {}
  }
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

// images: [{ dataUrl }] -- base64 data URLs read client-side (ChatView.vue's file input),
// never touching the server. Sent using the OpenAI vision content-array shape
// (content: [{type:'text',...}, {type:'image_url',...}]) instead of a plain string only
// when at least one image is attached, so a text-only message keeps the plain-string shape
// every existing stored session already uses -- no migration needed for old history.
// proxy.go forwards the whole request body as an untyped map[string]interface{}, so it never
// inspects or reshapes `content`; whether the model actually understands the image content
// depends entirely on that peer's own model (see ChatView.vue's attach-button tooltip).
async function send(text, images = []) {
  const trimmed = (text ?? '').trim()
  const session = activeSession.value
  if ((!trimmed && !images.length) || streaming.value || !session) return
  error.value = ''

  const content = images.length
    ? [
        ...(trimmed ? [{ type: 'text', text: trimmed }] : []),
        ...images.map((img) => ({ type: 'image_url', image_url: { url: img.dataUrl } })),
      ]
    : trimmed
  session.messages.push({ role: 'user', content })
  if (session.messages.length === 1) {
    const titleSrc = trimmed || (images.length ? `📷 ${images.length} image(s)` : '')
    session.title = titleSrc.substring(0, 28) + (titleSrc.length > 28 ? '…' : '')
  }
  saveSessions()

  const assistantMsg = { role: 'assistant', content: '', loading: true }
  session.messages.push(assistantMsg)
  // Re-read the just-pushed message back out through the reactive proxy (session came from
  // activeSession.value, itself read through the sessions ref's proxy) instead of mutating
  // the raw `assistantMsg` object literal directly below. Vue 3's reactivity only fires
  // dependency triggers on writes that go *through* a reactive proxy's set trap -- writing to
  // the raw closure reference bypasses that trap entirely, so nothing (the <think>-collapse
  // computed, the auto-scroll watcher, the template itself) ever re-evaluated mid-stream; the
  // whole reply only ever appeared to "pop in" once, on the next unrelated re-render.
  const liveMsg = session.messages[session.messages.length - 1]
  streaming.value = true

  let tokenCount = 0
  let genStartedAt = 0

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
            if (!genStartedAt) genStartedAt = performance.now()
            tokenCount++ // one SSE delta chunk == one generated token for vLLM's default streaming granularity
            liveMsg.content += delta
            liveMsg.loading = false
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
    liveMsg.content = ''
    error.value = e.message
  }

  if (tokenCount > 0 && genStartedAt) {
    const elapsedSec = (performance.now() - genStartedAt) / 1000
    if (elapsedSec > 0) liveMsg.tokensPerSec = tokenCount / elapsedSec
  }
  liveMsg.loading = false
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
