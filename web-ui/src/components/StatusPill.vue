<script setup>
// Reserved for real health/threshold signals only (never a decorative color
// for an arbitrary metric). Always icon + label together, per the
// "status color never carries meaning alone" rule.
const props = defineProps({
  variant: { type: String, default: 'good' }, // good | warning | critical
  label: { type: String, required: true },
})

const styles = {
  good: 'bg-emerald-50 text-emerald-700 border border-emerald-200/80',
  warning: 'bg-amber-50 text-amber-700 border border-amber-200/80',
  critical: 'bg-rose-50 text-rose-700 border border-rose-200/80',
}
const icons = { good: '●', warning: '▲', critical: '✕' }
</script>

<template>
  <span
    class="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-semibold shadow-xs"
    :class="styles[props.variant] || styles.good"
  >
    <span aria-hidden="true">{{ icons[props.variant] || icons.good }}</span>
    {{ label }}
  </span>
</template>
