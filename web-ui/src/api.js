// Thin fetch wrappers over the Go server's JSON API. Endpoint paths and
// response shapes are the contract with web.go / server_web.go -- keep this
// file in sync with those, not the other way around.

async function getJSON(url, opts) {
  const res = await fetch(url, opts)
  if (!res.ok) throw new Error(`${url} -> HTTP ${res.status}`)
  return res.json()
}

// --- Client-side endpoints (web.go, always available) ---
export const getNodeInfo = () => getJSON('/api/node_info')
export const getClusterStats = () => getJSON('/api/stats')
export const getPeers = () => getJSON('/api/peers')
export const getLocalStats = () => getJSON('/api/local_stats')
export const getLogs = () => getJSON('/api/logs')

export async function getConfig() {
  const res = await fetch('/api/config')
  if (!res.ok) throw new Error(`config fetch failed: HTTP ${res.status}`)
  return res.text()
}
export async function saveConfig(text) {
  const res = await fetch('/api/config', { method: 'POST', body: text })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.message || `save failed: HTTP ${res.status}`)
  return data
}
export const getBackups = () => getJSON('/api/config/backups')
export async function restoreBackup(filename) {
  const res = await fetch('/api/config/restore', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ filename }),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.message || `restore failed: HTTP ${res.status}`)
  return data
}

// --- Hub endpoints (server_web.go, only registered when server_mode.enabled) ---
// These are NOT same-origin: the dashboard is served from web_port, but the hub API is
// mounted on server_mode.proxy_port (see the 步驟 10 comment in web.go for why they were
// split). So they need an absolute URL with the hub port rather than a relative path --
// same host, different port. The Go side sends CORS headers for exactly this reason.
//
// 50008 matches the server_mode.proxy_port default, so hub pages work before
// /api/node_info resolves; setHubApiPort() corrects it afterwards if the hub uses a
// custom port.
let hubApiPort = 50008
export function setHubApiPort(port) {
  if (port > 0) hubApiPort = port
}
const hubURL = (path) => `${window.location.protocol}//${window.location.hostname}:${hubApiPort}${path}`

export const getHubPeers = () => getJSON(hubURL('/hub/api/peers'))
export const getHubEvents = () => getJSON(hubURL('/hub/api/events'))
export const getHubStats = () => getJSON(hubURL('/hub/api/stats'))
export const getHubLeaderboard = () => getJSON(hubURL('/hub/api/leaderboard'))
export const getHubTopology = () => getJSON(hubURL('/hub/api/cluster_topology'))
export async function hubForceRank() {
  return getJSON(hubURL('/hub/api/debug/force_rank'), { method: 'POST' })
}
export async function hubClearOffline() {
  return getJSON(hubURL('/hub/api/debug/clear_offline'), { method: 'POST' })
}

// --- Formatting helpers shared across views ---
export function fmtNum(n) {
  return (n || 0).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}
export function fmtUptime(s) {
  s = s || 0
  if (s < 60) return s + 's'
  if (s < 3600) return Math.floor(s / 60) + 'm ' + (s % 60) + 's'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h + 'h ' + m + 'm'
}
export function fmtMB(mb) {
  mb = mb || 0
  if (mb < 1024) return mb.toFixed(1) + 'MB'
  return (mb / 1024).toFixed(1) + 'GB'
}

// Parses a peer row's gpu_info JSON string (or falls back to the row itself
// if the API already returns parsed telemetry inline).
export function parseGpuInfo(peer) {
  if (typeof peer.gpu_info === 'string') {
    try {
      return JSON.parse(peer.gpu_info)
    } catch {
      return {}
    }
  }
  return peer
}
