<script setup>
import { useToast } from '../composables/useToast.js'

const { state } = useToast()

const variantClass = {
  good: 'bg-good',
  critical: 'bg-critical',
}
</script>

<template>
  <Transition
    enter-active-class="transition duration-300 ease-out"
    enter-from-class="translate-y-24 opacity-0"
    leave-active-class="transition duration-300 ease-in"
    leave-to-class="translate-y-24 opacity-0"
  >
    <div
      v-if="state.visible"
      class="fixed bottom-8 right-8 z-[1000] rounded-xl px-6 py-3.5 text-sm font-semibold text-white shadow-lg"
      :class="variantClass[state.variant] || variantClass.good"
    >
      {{ state.message }}
    </div>
  </Transition>
</template>
