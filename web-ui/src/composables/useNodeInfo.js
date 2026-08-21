import { reactive } from 'vue'
import { getNodeInfo } from '../api.js'

// Fetched once at app start and shared everywhere via this module-level
// singleton (no need for a full store library for three fields).
const state = reactive({
  localNodeId: '',
  serverHost: '127.0.0.1',
  hubModeEnabled: false,
  loaded: false,
})

let inFlight = null

export function useNodeInfo() {
  if (!state.loaded && !inFlight) {
    inFlight = getNodeInfo()
      .then((data) => {
        state.localNodeId = data.local_node_id || ''
        state.serverHost = data.server_host || '127.0.0.1'
        state.hubModeEnabled = !!data.hub_mode_enabled
        state.loaded = true
      })
      .catch(() => {})
  }
  return state
}
