<script lang="ts">
  import { safetyLabel } from '../lib/format'

  let { safety, label = false }: { safety: string; label?: boolean } = $props()
  const text = $derived(safetyLabel[safety] ?? 'Not recognised')
  const kind = $derived(safety || 'unknown')
</script>

<span class="safety" data-safety={kind} style:--c="var(--{kind})" title={text}>
  <span class="dot" aria-hidden="true"></span>
  {#if label}
    <span>{text}</span>
  {:else}
    <span class="sr-only">{text}</span>
  {/if}
</span>

<style>
  .safety {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--c);
    flex: none;
  }
  /* Shapes back the colours up for colour-blind users. */
  [data-safety='never'] .dot {
    border-radius: 1px;
  }
  [data-safety='delete-via-app'] .dot {
    border-radius: 1px;
    transform: rotate(45deg) scale(0.85);
  }
  [data-safety='unknown'] .dot {
    background: transparent;
    border: 1.5px solid var(--c);
  }
</style>
