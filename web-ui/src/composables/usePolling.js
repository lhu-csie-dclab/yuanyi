import { onMounted, onUnmounted } from 'vue'

// Runs `fn` immediately, then every `intervalMs`, stopping automatically when
// the owning component unmounts. Errors are swallowed by the caller's own
// try/catch (matches the original dashboard's fire-and-forget polling style).
export function usePolling(fn, intervalMs = 2000) {
  let handle = null
  onMounted(() => {
    fn()
    handle = setInterval(fn, intervalMs)
  })
  onUnmounted(() => {
    if (handle) clearInterval(handle)
  })
}
