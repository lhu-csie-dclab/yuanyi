import { reactive } from 'vue'

const state = reactive({ message: '', variant: 'good', visible: false })
let hideTimer = null

export function useToast() {
  function show(message, variant = 'good') {
    state.message = message
    state.variant = variant
    state.visible = true
    clearTimeout(hideTimer)
    hideTimer = setTimeout(() => {
      state.visible = false
    }, 3000)
  }
  return {
    state,
    success: (msg) => show(msg, 'good'),
    error: (msg) => show(msg, 'critical'),
  }
}
