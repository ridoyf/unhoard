<script lang="ts">
  import type { Problem } from '../lib/api'
  import { applyFix } from '../lib/actions'

  let {
    problem,
    path = '',
    onretry,
  }: {
    problem: Problem
    path?: string
    onretry?: () => void // shows "Try again" when given
  } = $props()

  const fixes = $derived(problem.fixes ?? [])
  const advice = $derived(fixes.filter((f) => !f.kind))
  const buttons = $derived(fixes.filter((f) => f.kind && (f.kind !== 'retry' || onretry)))
</script>

<div class="problem" role="alert">
  <p>{problem.summary}</p>
  {#if advice.length}
    <ul>
      {#each advice as f}
        <li>{f.label}</li>
      {/each}
    </ul>
  {/if}
  {#if buttons.length}
    <div class="toolbar">
      {#each buttons as f}
        <button onclick={() => applyFix(f, { path, retry: onretry })}>{f.label}</button>
      {/each}
    </div>
  {/if}
  {#if problem.detail}
    <details>
      <summary class="muted">What Windows said</summary>
      <pre>{problem.detail}</pre>
    </details>
  {/if}
</div>

<style>
  .problem {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 12px;
    border-left: 3px solid var(--never);
    background: color-mix(in srgb, var(--never) 7%, var(--surface));
    border-radius: 0 var(--radius-s) var(--radius-s) 0;
  }
  ul {
    margin: 0;
    padding-left: 18px;
  }
  summary {
    cursor: pointer;
    font-size: var(--text-m);
  }
  pre {
    margin: 4px 0 0;
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
