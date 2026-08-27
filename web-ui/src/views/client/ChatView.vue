<script setup>
import { ref, computed, nextTick, onMounted, watch } from 'vue'
import { useNodeInfo } from '../../composables/useNodeInfo.js'
import { useI18n } from '../../composables/useI18n.js'

// ── Config ──────────────────────────────────────────────────────────────────
// The endpoint to call. Default = same-origin proxy through yuanyi Go server.
// User can override to any local vLLM / Ollama / OpenAI-compatible URL.
const STORAGE_KEY_SESSIONS = 'chat_sessions_v1'
const STORAGE_KEY_CONFIG   = 'chat_config_v1'
const STORAGE_KEY_ENDPOINT_OVERRIDDEN = 'chat_endpoint_overridden_v1'

// ── Reactive state ───────────────────────────────────────────────────────────
const nodeInfo = useNodeInfo()
const { t }    = useI18n()

const sessions  = ref([])          // [{ id, title, model, messages: [] }]
const activeId  = ref(null)
const input     = ref('')
const streaming = ref(false)
const error     = ref('')
const showConfig = ref(false)
const messagesEl = ref(null)

// Config (persisted)
// Default port 50006 is this project's own documented default gateway/proxy port (see
// config.go / install.ps1 / install.sh), matching the comment above: go through the Go
// server's gateway, not vLLM's raw port. The onMounted auto-detect below corrects this to
// the *actual* configured proxy_port once /api/node_info loads, in case it was customized
// during install -- this static value is only the fallback shown before that resolves (or
// if the user has an override flag set from an unrelated earlier session on this browser
// origin). Pointing this at vLLM's raw port (8100) instead was a real bug: 8100 is the one
// port a relay-only node never listens on at all (it runs no local vLLM by design), so the
// chat page failed with "Failed to fetch" for exactly the operators most likely to try it
// first -- and for an inference node, it also failed during the vLLM warm-up window even
// though the gateway (50006) would have queued the request correctly the whole time.
const cfg = ref({
  endpoint: 'http://localhost:50006/v1/chat/completions',
  model:    '',
  systemPrompt: '',
  temperature: 0.7,
  maxTokens: 2048,
  apiKey: '',
})

// ── Computed ─────────────────────────────────────────────────────────────────
const activeSession = computed(() =>
  sessions.value.find(s => s.id === activeId.value) || null
)
const messages = computed(() => activeSession.value?.messages || [])

// ── Persistence ──────────────────────────────────────────────────────────────
function saveSessions() {
  localStorage.setItem(STORAGE_KEY_SESSIONS, JSON.stringify(sessions.value))
}
function loadSessions() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_SESSIONS)
    if (raw) sessions.value = JSON.parse(raw)
  } catch {}
  if (!sessions.value.length) newSession()
  else activeId.value = sessions.value[0].id
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

// ── Session management ────────────────────────────────────────────────────────
let _sid = Date.now()
function newSession() {
  const id = String(++_sid)
  sessions.value.unshift({ id, title: '新對話', model: cfg.value.model, messages: [] })
  activeId.value = id
  saveSessions()
}
function deleteSession(id) {
  sessions.value = sessions.value.filter(s => s.id !== id)
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
  const s = sessions.value.find(s => s.id === id)
  if (s) { s.title = title; saveSessions() }
}

// ── Auto-scroll ───────────────────────────────────────────────────────────────
async function scrollBottom() {
  await nextTick()
  if (messagesEl.value) {
    messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  }
}

// ── Markdown-ish renderer (simple, no deps) ──────────────────────────────────
function renderMd(text) {
  if (!text) return ''
  return text
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    // code blocks
    .replace(/```([\w]*)\n?([\s\S]*?)```/g,
      (_, lang, code) => `<pre class="code-block"><code class="lang-${lang}">${code.trim()}</code></pre>`)
    // inline code
    .replace(/`([^`]+)`/g, '<code class="inline-code">$1</code>')
    // bold
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    // italic
    .replace(/\*(.+?)\*/g, '<em>$1</em>')
    // line breaks
    .replace(/\n/g, '<br>')
}

// ── Send message ─────────────────────────────────────────────────────────────
async function send() {
  const text = input.value.trim()
  if (!text || streaming.value || !activeSession.value) return
  error.value = ''

  // Push user message
  activeSession.value.messages.push({ role: 'user', content: text })
  // Auto-title from first message
  if (activeSession.value.messages.length === 1) {
    activeSession.value.title = text.substring(0, 28) + (text.length > 28 ? '…' : '')
  }
  input.value = ''
  scrollBottom()

  // Build assistant placeholder
  const assistantMsg = { role: 'assistant', content: '', loading: true }
  activeSession.value.messages.push(assistantMsg)
  scrollBottom()
  streaming.value = true

  // Build messages array for API (include system prompt if set)
  const apiMessages = []
  if (cfg.value.systemPrompt) {
    apiMessages.push({ role: 'system', content: cfg.value.systemPrompt })
  }
  apiMessages.push(...activeSession.value.messages
    .filter(m => !m.loading)
    .map(m => ({ role: m.role, content: m.content }))
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

    // Stream SSE chunks
    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''

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
            scrollBottom()
          }
        } catch {}
      }
    }
  } catch (e) {
    assistantMsg.content = ''
    assistantMsg.loading = false
    error.value = e.message
  }

  assistantMsg.loading = false
  streaming.value = false
  saveSessions()
  scrollBottom()
}

function handleKey(e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

function clearSession() {
  if (!activeSession.value) return
  activeSession.value.messages = []
  saveSessions()
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────
onMounted(() => {
  loadCfg()
  loadSessions()
  scrollBottom()

  // Auto-configure endpoint from config.json via /api/node_info.
  // We only overwrite the default if the user has NOT previously saved a
  // custom endpoint (tracked by a separate localStorage flag).
  const hasOverride = localStorage.getItem(STORAGE_KEY_ENDPOINT_OVERRIDDEN)
  if (!hasOverride) {
    // nodeInfo may not be loaded yet — watch until it is
    const stopWatch = watch(
      () => nodeInfo.loaded,
      (loaded) => {
        if (!loaded) return
        stopWatch()
        // Use proxy port (goes through yuanyi load balancer) as primary.
        // vllm port is direct; proxy is preferred for production.
        const host = window.location.hostname || '127.0.0.1'
        const endpoint = `http://${host}:${nodeInfo.proxyPort}/v1/chat/completions`
        cfg.value.endpoint = endpoint
        if (nodeInfo.modelName) cfg.value.model = nodeInfo.modelName
        saveCfg()
      },
      { immediate: true }
    )
  }
})
watch(cfg, saveCfg, { deep: true })
</script>

<template>
  <div class="flex h-[calc(100vh-0px)] overflow-hidden bg-surface">

    <!-- ═══ Session Sidebar ═══════════════════════════════════════════════════ -->
    <aside class="hidden sm:flex w-56 shrink-0 flex-col border-r border-border bg-surface-card">
      <div class="flex items-center justify-between px-4 py-3.5 border-b border-white/5">
        <span class="text-xs font-bold uppercase tracking-widest text-ink-faint">{{ t('chat_sessions') }}</span>
        <button
          class="flex h-7 w-7 items-center justify-center rounded-lg bg-brand text-white hover:bg-brand-light transition-colors text-sm font-bold"
          :title="t('chat_new')"
          @click="newSession"
        >+</button>
      </div>
      <div class="flex-1 overflow-y-auto py-1.5 px-2 space-y-0.5">
        <button
          v-for="s in sessions"
          :key="s.id"
          class="group w-full flex items-center gap-2 rounded-xl px-3 py-2 text-left text-xs transition-colors"
          :class="s.id === activeId
            ? 'bg-brand/10 text-brand-light font-semibold'
            : 'text-ink-muted hover:bg-white/5'"
          @click="selectSession(s.id)"
        >
          <span class="truncate flex-1">{{ s.title }}</span>
          <span
            class="hidden group-hover:flex h-5 w-5 items-center justify-center rounded-md hover:bg-rose-100 hover:text-rose-500 text-ink-faint text-[0.7rem]"
            @click.stop="deleteSession(s.id)"
          >✕</span>
        </button>
      </div>
      <!-- Config button -->
      <div class="border-t border-white/5 p-2">
        <button
          class="w-full flex items-center gap-2 rounded-xl px-3 py-2 text-xs text-ink-faint hover:bg-white/5 transition-colors"
          @click="showConfig = !showConfig"
        >
          <span>⚙️</span> {{ t('chat_settings') }}
        </button>
      </div>
    </aside>

    <!-- ═══ Main Chat ════════════════════════════════════════════════════════ -->
    <div class="flex flex-1 flex-col min-w-0 overflow-hidden">

      <!-- Header -->
      <div class="flex items-center justify-between border-b border-border bg-surface-card px-5 py-3 shrink-0">
        <div class="flex items-center gap-3">
          <!-- Mobile new chat button -->
          <button class="sm:hidden flex h-8 w-8 items-center justify-center rounded-lg bg-brand text-white text-sm font-bold" @click="newSession">+</button>
          <div>
            <div class="font-semibold text-ink text-sm truncate max-w-[200px]">
              {{ activeSession?.title || t('page_chat') }}
            </div>
            <div class="text-[0.68rem] text-ink-faint font-mono">{{ cfg.endpoint }}</div>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <span v-if="streaming" class="pill pill-blue animate-pulse">{{ t('chat_streaming') }}</span>
          <button
            class="btn btn-ghost btn-sm text-ink-faint"
            :title="t('chat_clear')"
            @click="clearSession"
          >🗑 {{ t('chat_clear') }}</button>
          <button
            class="btn btn-ghost btn-sm"
            :class="showConfig ? 'bg-brand/10 text-brand-light' : ''"
            @click="showConfig = !showConfig"
          >⚙️</button>
        </div>
      </div>

      <!-- Config panel -->
      <Transition name="slide-down">
        <div v-if="showConfig" class="shrink-0 border-b border-border bg-white/[0.04] px-5 py-4">
          <!-- Source hint -->
          <div class="mb-3 flex items-center justify-between">
            <div class="flex items-center gap-2 text-[0.68rem] text-ink-faint">
              <span class="h-1.5 w-1.5 rounded-full" :class="nodeInfo.loaded ? 'bg-emerald-400' : 'bg-amber-400 animate-pulse'" />
              <span v-if="nodeInfo.loaded">
                {{ t('cfg_from_config', nodeInfo.proxyPort, nodeInfo.vllmPort, nodeInfo.modelName) }}
              </span>
              <span v-else>{{ t('cfg_reading') }}</span>
            </div>
            <button
              class="text-[0.68rem] text-cyan hover:text-brand-light font-semibold"
              :class="{ 'opacity-40 cursor-not-allowed': !nodeInfo.loaded }"
              :disabled="!nodeInfo.loaded"
              @click="() => {
                localStorage.removeItem(STORAGE_KEY_ENDPOINT_OVERRIDDEN)
                const host = window.location.hostname || '127.0.0.1'
                cfg.endpoint = `http://${host}:${nodeInfo.proxyPort}/v1/chat/completions`
                cfg.model = nodeInfo.modelName || ''
                saveCfg()
              }"
            >{{ t('cfg_reset') }}</button>
          </div>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 max-w-3xl">
            <label class="flex flex-col gap-1">
              <span class="text-[0.68rem] font-bold uppercase tracking-wider text-ink-faint">API Endpoint</span>
              <input
                v-model="cfg.endpoint"
                class="chat-input text-xs font-mono"
                placeholder="http://localhost:8100/v1/chat/completions"
                @change="localStorage.setItem(STORAGE_KEY_ENDPOINT_OVERRIDDEN, '1')"
              />
            </label>
            <label class="flex flex-col gap-1">
              <span class="text-[0.68rem] font-bold uppercase tracking-wider text-ink-faint">Model</span>
              <input v-model="cfg.model" class="chat-input text-xs font-mono" placeholder="留空 = server default" />
            </label>
            <label class="flex flex-col gap-1">
              <span class="text-[0.68rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('cfg_api_key') }}</span>
              <input v-model="cfg.apiKey" type="password" class="chat-input text-xs font-mono" placeholder="sk-..." />
            </label>
            <label class="flex flex-col gap-1">
              <span class="text-[0.68rem] font-bold uppercase tracking-wider text-ink-faint">Temperature</span>
              <div class="flex items-center gap-2">
                <input v-model.number="cfg.temperature" type="range" min="0" max="2" step="0.05" class="flex-1 accent-brand" />
                <span class="text-xs font-mono w-8 text-right text-ink-muted">{{ cfg.temperature }}</span>
              </div>
            </label>
            <label class="flex flex-col gap-1">
              <span class="text-[0.68rem] font-bold uppercase tracking-wider text-ink-faint">Max Tokens</span>
              <input v-model.number="cfg.maxTokens" type="number" class="chat-input text-xs font-mono" min="64" max="32768" step="64" />
            </label>
            <label class="flex flex-col gap-1 sm:col-span-2 lg:col-span-1">
              <span class="text-[0.68rem] font-bold uppercase tracking-wider text-ink-faint">{{ t('cfg_system') }}</span>
              <textarea v-model="cfg.systemPrompt" class="chat-input text-xs resize-none" rows="2" placeholder="You are a helpful assistant." />
            </label>
          </div>
        </div>
      </Transition>

      <!-- Error banner -->
      <div v-if="error" class="shrink-0 flex items-center gap-2 bg-rose-400/10 border-b border-rose-400/30 px-5 py-2.5 text-sm text-rose-300">
        <span>⚠️</span>
        <span class="flex-1 truncate">{{ error }}</span>
        <button class="text-rose-400 hover:text-rose-600 font-bold" @click="error = ''">✕</button>
      </div>

      <!-- Messages -->
      <div ref="messagesEl" class="flex-1 overflow-y-auto px-4 py-5 space-y-4">
        <!-- Empty state -->
        <div v-if="!messages.length" class="flex h-full items-center justify-center">
          <div class="text-center space-y-2">
            <div class="text-4xl">💬</div>
            <div class="text-ink-faint text-sm font-medium">{{ t('chat_empty_title') }}</div>
            <div class="text-ink-faint text-xs font-mono">{{ cfg.endpoint }}</div>
          </div>
        </div>

        <div
          v-for="(msg, i) in messages"
          :key="i"
          class="flex gap-3"
          :class="msg.role === 'user' ? 'justify-end' : 'justify-start'"
        >
          <!-- Avatar -->
          <div
            v-if="msg.role !== 'user'"
            class="h-7 w-7 shrink-0 rounded-lg flex items-center justify-center text-sm mt-0.5"
            :class="msg.loading ? 'bg-brand/15 animate-pulse' : 'bg-brand'"
          >
            <span v-if="!msg.loading" class="text-white text-xs font-bold">AI</span>
            <span v-else>⋯</span>
          </div>

          <!-- Bubble -->
          <div
            class="max-w-[75%] rounded-2xl px-4 py-2.5 text-sm leading-relaxed"
            :class="msg.role === 'user'
              ? 'bg-brand text-white rounded-br-sm'
              : 'bg-surface-card border border-border text-ink rounded-bl-sm shadow-xs'"
          >
            <div v-if="msg.role === 'user'" class="whitespace-pre-wrap">{{ msg.content }}</div>
            <div
              v-else-if="msg.loading"
              class="flex items-center gap-1.5 text-ink-faint"
            >
              <span class="animate-bounce" style="animation-delay:0ms">●</span>
              <span class="animate-bounce" style="animation-delay:150ms">●</span>
              <span class="animate-bounce" style="animation-delay:300ms">●</span>
            </div>
            <div
              v-else
              class="chat-md"
              v-html="renderMd(msg.content)"
            />
          </div>

          <!-- User avatar -->
          <div
            v-if="msg.role === 'user'"
            class="h-7 w-7 shrink-0 rounded-lg bg-white/10 flex items-center justify-center text-xs font-bold text-ink-muted mt-0.5"
          >{{ t('chat_user_me') }}</div>
        </div>
      </div>

      <!-- Input area -->
      <div class="shrink-0 border-t border-border bg-surface-card px-4 py-3.5">
        <div class="flex items-end gap-3 max-w-3xl mx-auto">
          <textarea
            v-model="input"
            class="chat-input flex-1 resize-none max-h-36 min-h-[2.5rem]"
            rows="1"
            :placeholder="t('chat_placeholder')"
            :disabled="streaming"
            @keydown="handleKey"
            @input="$event.target.style.height = 'auto'; $event.target.style.height = $event.target.scrollHeight + 'px'"
          />
          <button
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-brand text-white shadow-sm transition-all hover:bg-brand-light hover:shadow-md disabled:opacity-40 disabled:cursor-not-allowed"
            :disabled="!input.trim() || streaming"
            @click="send"
          >
            <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
              <path d="M22 2 11 13M22 2 15 22 11 13 2 9l20-7z"/>
            </svg>
          </button>
        </div>
        <div class="text-center mt-1.5 text-[0.65rem] text-ink-faint">
          {{ t('chat_storage_hint') }}
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
@reference "../../style.css";

.chat-input {
  @apply w-full rounded-xl border border-border bg-white/[0.04] px-3 py-2 text-sm text-ink outline-none transition-all;
  @apply focus:border-blue-400 focus:bg-surface-card focus:ring-2 focus:ring-blue-100;
}

/* Markdown content */
.chat-md :deep(pre.code-block) {
  background: #0f172a;
  color: #a3e635;
  border-radius: 0.75rem;
  padding: 0.875rem 1rem;
  overflow-x: auto;
  margin: 0.5rem 0;
  font-size: 0.8rem;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  line-height: 1.6;
}
.chat-md :deep(code.inline-code) {
  background: #f1f5f9;
  color: #3b5bdb;
  border-radius: 0.25rem;
  padding: 0.1em 0.35em;
  font-size: 0.82em;
  font-family: ui-monospace, monospace;
}

/* Slide transition */
.slide-down-enter-active, .slide-down-leave-active {
  transition: all 0.2s ease;
}
.slide-down-enter-from, .slide-down-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
