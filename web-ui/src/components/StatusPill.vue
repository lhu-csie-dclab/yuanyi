<script setup>
// Reserved for real health/threshold signals only (never a decorative color
// for an arbitrary metric). Always icon + label together, per the
// "status color never carries meaning alone" rule.
const props = defineProps({
  variant: { type: String, default: 'good' }, // good | warning | critical
  label: { type: String, required: true },
})

const styles = {
  good: 'bg-good/15 text-good',
  warning: 'bg-warning/15 text-warning',
  critical: 'bg-critical/15 text-critical',
}
const icons = { good: '●', warning: '▲', critical: '✕' }
</script>

<template>
  <span
    class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-semibold"
    :class="styles[props.variant] || styles.good"
  >
    <span aria-hidden="true">{{ icons[props.variant] || icons.good }}</span>
    {{ label }}
  </span>
</template>
