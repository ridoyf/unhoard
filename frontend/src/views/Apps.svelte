<script lang="ts">
  import { api, type AppFootprint } from '../lib/api'
  import { bytes, plural } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { askAction, reveal, ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'

  const res = loader(async () => (await api.Apps()) ?? [])
  let filter = $state('')

  const shown = $derived(
    (res.data ?? []).filter(
      (a) => !filter || `${a.name} ${a.publisher}`.toLowerCase().includes(filter.toLowerCase()),
    ),
  )

  // The installer's own figure is shown, marked as approximate, until a scan measures the folder.
  function programSize(a: AppFootprint): string {
    if (a.installBytes) return bytes(a.installBytes)
    return a.estimatedBytes ? `about ${bytes(a.estimatedBytes)}` : ''
  }

  // Matches the backend's ranking: what the scan measured, or what the installer claims if more.
  function size(a: AppFootprint): number {
    return Math.max(a.installBytes + a.dataBytes, a.estimatedBytes)
  }
</script>

<div class="view">
  <header>
    <h1>Apps</h1>
    <p class="muted">
      Each program plus the AppData and ProgramData folders that look like its own.
      {#if !ui.scan}Until you scan, sizes are what each installer reported.{/if}
      Uninstall runs the app's own uninstaller.
    </p>
  </header>
  <div class="toolbar">
    <input type="search" bind:value={filter} placeholder="Filter apps" aria-label="Filter apps" />
    <span class="muted">{plural(shown.length, 'app')}</span>
  </div>
  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if !res.data}
    <Busy label="Reading installed apps" />
  {:else}
    <table class="data-table">
      <thead>
        <tr>
          <th>App</th>
          <th class="num">Program</th>
          <th class="num">Data</th>
          <th class="num">Total</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each shown as a (a.key)}
          <tr>
            <td>
              <div class="name">{a.name}</div>
              <div class="muted small">{[a.publisher, a.version].filter(Boolean).join(', ')}</div>
              {#each a.dataFolders ?? [] as d}
                <button class="quiet folder" onclick={() => reveal(d)} disabled={!ui.scan}>
                  {d}
                </button>
              {/each}
            </td>
            <td class="num">{programSize(a)}</td>
            <td class="num">{a.dataBytes ? bytes(a.dataBytes) : ''}</td>
            <td class="num"><strong>{size(a) ? bytes(size(a)) : ''}</strong></td>
            <td class="num">
              {#if a.installLocation && ui.scan}
                <button class="quiet" onclick={() => reveal(a.installLocation)}>
                  Go to folder
                </button>
              {/if}
              <button onclick={() => askAction('uninstall-app', [a.key])}>Uninstall…</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .name {
    font-weight: 600;
  }
  .folder {
    display: block;
    font-size: var(--text-s);
    padding: 0;
    min-height: 0;
    color: var(--accent);
    text-align: left;
    word-break: break-all;
  }
</style>
