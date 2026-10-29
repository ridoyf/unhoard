<script lang="ts">
  import { api } from '../lib/api'
  import { bytes, plural } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { askAction, attempt, ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'

  const res = loader(() => api.HiddenSpace())
  const h = $derived(res.data)
  const unreadable = $derived(
    h && h.inaccessible > 0
      ? `Includes ${plural(h.inaccessible, 'folder')} Windows would not let you read. `
      : '',
  )

  const gapItems = $derived((h?.items ?? []).filter((i) => !i.inScan))
  const visibleItems = $derived((h?.items ?? []).filter((i) => i.inScan))
</script>

<div class="view">
  {#if res.error}
    <header><h1>Hidden space</h1></header>
    <p class="error-text" role="alert">{res.error}</p>
  {:else if h}
    <header>
      <h1>Windows says {bytes(h.used)} is used. The scan found {bytes(h.scanned)}.</h1>
      <p class="muted">
        The difference, {bytes(h.gap)}, is space that no file listing shows. Here is where it goes.
      </p>
    </header>

    <div class="split-bar" aria-hidden="true">
      <span class="found" style:flex={h.scanned}></span>
      {#each gapItems.filter((i) => i.known && i.bytes > 0) as i}
        <span class="gap" style:flex={i.bytes} title={i.name}></span>
      {/each}
      <span class="unknown" style:flex={h.unexplained}></span>
    </div>

    <section>
      <h2>Not in the scan</h2>
      <ul>
        {#each gapItems as i}
          <li>
            <div>
              <h3>{i.name}</h3>
              <p class="muted">{i.description}</p>
            </div>
            <span class="amount">
              {i.known ? bytes(i.bytes) : i.needsAdmin ? 'Needs admin to measure' : 'Not reported'}
            </span>
            <div class="acts">
              {#if i.needsAdmin && !ui.status?.elevated}
                <button onclick={() => attempt(api.RelaunchAsAdmin())}>
                  Restart as administrator
                </button>
              {/if}
              {#if i.settings}
                <button onclick={() => attempt(api.OpenSettings(i.settings))}>Open settings</button>
              {/if}
              {#if i.action}
                <button class="primary" onclick={() => askAction(i.action)}>Preview…</button>
              {/if}
            </div>
          </li>
        {/each}
        <li>
          <div>
            <h3>Unexplained</h3>
            <p class="muted">
              {unreadable}Also files that changed during the scan and other system areas.
            </p>
          </div>
          <span class="amount">{bytes(h.unexplained)}</span>
          <div class="acts"></div>
        </li>
      </ul>
    </section>

    <section>
      <h2>In the scan, but easy to miss</h2>
      <ul>
        {#each visibleItems as i}
          <li>
            <div>
              <h3>{i.name}</h3>
              <p class="muted">{i.description}</p>
            </div>
            <span class="amount">{bytes(i.bytes)}</span>
            <div class="acts">
              {#if i.settings}
                <button onclick={() => attempt(api.OpenSettings(i.settings))}>Open settings</button>
              {/if}
              {#if i.action && i.bytes > 0}
                <button class="primary" onclick={() => askAction(i.action)}>Preview…</button>
              {/if}
            </div>
          </li>
        {:else}
          <li><p class="muted">None on this drive.</p></li>
        {/each}
      </ul>
    </section>
  {:else}
    <Busy label="Asking Windows about restore points, shadow copies and reserved space" />
  {/if}
</div>

<style>
  header h1 {
    max-width: 30ch;
  }
  .split-bar {
    display: flex;
    height: 16px;
    border-radius: var(--radius-m);
    overflow: hidden;
    max-width: 920px;
    gap: 2px;
    background: var(--sunken);
  }
  .found {
    background: var(--ink);
    opacity: 0.7;
  }
  .gap {
    background: var(--delete-via-app);
  }
  .unknown {
    background: repeating-linear-gradient(135deg, var(--unknown) 0 3px, transparent 3px 6px);
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-width: 920px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    border-top: 1px solid var(--line);
  }
  li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 160px 220px;
    gap: 16px;
    align-items: center;
    padding: 12px 0;
    border-bottom: 1px solid var(--line);
  }
  .amount {
    text-align: right;
  }
  .acts {
    display: flex;
    gap: 6px;
    justify-content: flex-end;
    flex-wrap: wrap;
  }
</style>
