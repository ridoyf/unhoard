<script lang="ts">
  import { api } from '../lib/api'
  import { loader } from '../lib/load.svelte'
  import { askAction, attempt, go, ui } from '../lib/state.svelte'
  import { pickerView } from '../lib/actions'

  const res = loader(async () => (await api.Actions()) ?? [])
</script>

<div class="view">
  <header>
    <h1>Cleanup tools</h1>
    <p class="muted">
      Windows' own tools and the apps' own commands. Each shows the exact command before it runs.
    </p>
  </header>
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {/if}
  <ul>
    {#each res.data ?? [] as a}
      <li>
        <div>
          <h2>{a.title}</h2>
          <p class="muted">{a.description}</p>
          {#if a.needsAdmin && !ui.status?.elevated}
            <p class="admin">Needs administrator rights.</p>
          {/if}
        </div>
        {#if pickerView[a.id]}
          <button onclick={() => go(pickerView[a.id].view)}>{pickerView[a.id].label}</button>
        {:else}
          <button class="primary" onclick={() => askAction(a.id)}>Preview…</button>
        {/if}
      </li>
    {/each}
  </ul>
  {#if ui.status && !ui.status.elevated}
    <div class="toolbar">
      <button onclick={() => attempt(api.RelaunchAsAdmin())}>Restart as administrator</button>
    </div>
  {/if}
  <div class="toolbar">
    <button class="quiet" onclick={() => attempt(api.OpenSettings('ms-settings:storagesense'))}>
      Open Windows Storage Sense
    </button>
  </div>
</div>

<style>
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    max-width: 920px;
    border-top: 1px solid var(--line);
  }
  li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 20px;
    align-items: center;
    padding: 16px 0;
    border-bottom: 1px solid var(--line);
  }
  li p {
    max-width: 72ch;
  }
  .admin {
    font-size: var(--text-m);
    font-weight: 600;
  }
</style>
