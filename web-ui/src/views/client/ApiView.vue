<script setup>
import PageHeader from '../../components/PageHeader.vue'
</script>

<template>
  <PageHeader title="OpenAI /v1 API" />

  <div class="p-8">
    <div class="card">
      <h2 class="mb-2 text-sm font-semibold">Local OpenAI Compatible API Gateway</h2>
      <p class="mb-4 text-sm text-ink-muted">
        This node's local task dispatcher is active and accepts OpenAI-compatible
        <code class="rounded bg-white/10 px-1 py-0.5 font-mono text-xs">/v1</code> requests,
        automatically routing them across the swarm.
      </p>
      <div class="code-panel !text-lime-400">
        <div class="font-bold">POST /v1/chat/completions</div>
        <div class="font-bold">POST /v1/completions</div>
        <br />
        <div class="text-ink-muted">Example:</div>
        <pre class="whitespace-pre-wrap">curl http://localhost:50006/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"Qwen3-4B-AWQ","messages":[{"role":"user","content":"Hello!"}]}'</pre>
      </div>
    </div>
  </div>
</template>
