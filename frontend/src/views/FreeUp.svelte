<script lang="ts">
  import { api } from '../lib/api'
  import { bytes } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { askAction, go, isView } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'

  const res = loader(async () => (await api.Summary()) ?? [])
  const items = $derived(res.data)

  // Quarantine is space already removed; duplicates need a person's judgement. Neither is "free now".
  const ready = $derived(
    (items ?? []).filter((i) => i.id !== 'quarantine' && i.id !== 'duplicates'),
  )
  const total = $derived(ready.reduce((s, i) => s + i.bytes, 0))
  const largest = $derived(Math.max(1, ...(items ?? []).map((i) => i.bytes)))
</script>

<div class="view">
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if items}
    <header>
      <h1>You can free about {bytes(total)}</h1>
      <p class="muted">Known safe removals and Windows cleanups, biggest first.</p>
    </header>

    <ul class="items">
      {#each items as it}
        <li>
          <div class="what">
            <h2>{it.title}</h2>
            <p class="muted">{it.detail}</p>
          </div>
          <div class="amount">
            <span class="bar" aria-hidden="true">
              <span style:width="{(it.bytes / largest) * 100}%"></span>
            </span>
            <strong>{bytes(it.bytes)}</strong>
          </div>
          {#if it.action}
            <button class="primary" onclick={() => askAction(it.action)}>Preview…</button>
          {:else if isView(it.view)}
            {@const view = it.view}
            <button onclick={() => go(view)}>Review</button>
          {/if}
        </li>
      {:else}
        <li>
          <p class="muted">
            Nothing obvious to clear. Try Duplicates or Big and stale to review your own files.
          </p>
        </li>
      {/each}
    </ul>

    <p class="muted hint">
      Your own files are not counted here. Look through <button
        class="quiet inline"
        onclick={() => go('stale')}
      >
        Big and stale
      </button>
      and
      <button class="quiet inline" onclick={() => go('duplicates')}>Duplicates</button>
      to decide about those.
    </p>
  {:else}
    <Busy label="Adding up what can be freed" />
  {/if}
</div>

<style>
  .items {
    list-style: none;
    margin: 0;
    padding: 0;
    max-width: 920px;
    border-top: 1px solid var(--line);
  }
  li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 260px 90px;
    gap: 20px;
    align-items: center;
    padding: 16px 0;
    border-bottom: 1px solid var(--line);
  }
  .amount {
    display: flex;
    align-items: center;
    gap: 12px;
    justify-content: flex-end;
  }
  .bar {
    flex: 1;
    height: 8px;
    background: var(--sunken);
    border-radius: var(--radius-s);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--safe);
  }
  strong {
    min-width: 72px;
    text-align: right;
  }
  .hint {
    max-width: 72ch;
  }
  .inline {
    padding: 0 2px;
    min-height: 0;
    color: var(--accent);
  }
</style>
