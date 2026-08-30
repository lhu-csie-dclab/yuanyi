<script setup>
import PageHeader from '../../components/PageHeader.vue'
</script>

<template>
  <PageHeader title="OpenAI /v1 API" />

  <div class="space-y-4 sm:space-y-6 p-4 sm:p-6 lg:p-8 max-w-full min-w-0">
    <div class="card min-w-0">
      <h2 class="mb-2 text-xs sm:text-sm font-semibold text-white">Local OpenAI Compatible API Gateway</h2>
      <p class="mb-4 text-xs sm:text-sm text-ink-muted leading-relaxed">
        This node's local task dispatcher is active and accepts OpenAI-compatible
        <code class="rounded bg-white/10 px-1 py-0.5 font-mono text-xs text-brand-light">/v1</code> requests,
        automatically routing them across the swarm.
      </p>
      <div class="code-panel overflow-x-auto text-xs sm:text-sm !text-lime-400">
        <div class="font-bold">POST /v1/chat/completions</div>
        <div class="font-bold">POST /v1/completions</div>
        <br />
        <div class="text-ink-muted">Example:</div>
        <pre class="whitespace-pre-wrap break-all font-mono">curl http://localhost:50006/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"cyankiwi/Qwen3-VL-4B-Instruct-AWQ-4bit","messages":[{"role":"user","content":"Hello!"}]}'</pre>
      </div>
    </div>
  </div>
</template>
