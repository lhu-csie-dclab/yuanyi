<script setup>
const props = defineProps({
  variant: { type: String, default: 'good' },
  label: { type: String, required: true },
})
const map = {
  good:     'pill-green',
  warning:  'pill-amber',
  critical: 'pill-red',
}
const dot = { good: '●', warning: '▲', critical: '✕' }
</script>

<template>
  <span class="pill" :class="map[props.variant] || map.good">
    <span>{{ dot[props.variant] || dot.good }}</span>
    {{ label }}
  </span>
</template>
