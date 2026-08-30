import { reactive } from 'vue'
import { getNodeInfo } from '../api.js'

// Fetched once at app start and shared everywhere via this module-level
// singleton (no need for a full store library for a handful of fields).
const state = reactive({
  localNodeId: '',
  serverHost: '127.0.0.1',
  hubModeEnabled: false,
  relayOnly: false,
  // vLLM / proxy info derived from config.json (exposed by /api/node_info)
  vllmPort: 8100,
  proxyPort: 50006,
  modelName: '',
  loaded: false,
})

let inFlight = null

export function useNodeInfo() {
  if (!state.loaded && !inFlight) {
    inFlight = getNodeInfo()
      .then((data) => {
        state.localNodeId    = data.local_node_id    || ''
        state.serverHost     = data.server_host      || '127.0.0.1'
        state.hubModeEnabled = !!data.hub_mode_enabled
        state.relayOnly      = !!data.relay_only
        state.vllmPort       = data.vllm_port        || 8100
        state.proxyPort      = data.proxy_port       || 50006
        state.modelName      = data.model_name       || ''
        state.loaded         = true
      })
      .catch(() => {})
  }
  return state
}
