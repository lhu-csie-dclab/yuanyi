import { reactive } from 'vue'
import { getNodeInfo, setHubApiPort } from '../api.js'

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
  // Port this node serves /hub/api/* on (server_mode.proxy_port) -- the hub API is not on
  // web_port any more, so api.js needs it to build absolute URLs.
  hubApiPort: 50008,
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
        state.hubApiPort     = data.hub_api_port     || 50008
        setHubApiPort(state.hubApiPort)
        state.loaded         = true
      })
      .catch(() => {})
  }
  return state
}
