<script lang="ts">
  import { api } from '../lib/api'
  import { bytes, errorText } from '../lib/format'
  import { attempt, refreshStatus, startScan, ui } from '../lib/state.svelte'

  $effect(() => {
    attempt(refreshStatus())
  })

  async function pickFolder() {
    try {
      const dir = await api.PickFolder()
      if (dir) startScan(dir)
    } catch (e) {
      ui.scanError = errorText(e)
    }
  }
</script>

<div class="view">
  <header>
    <h1>Where did my space go?</h1>
    <p class="muted">
      Scan a drive to see what is using it, what each thing is, and what you can safely remove.
      Nothing is deleted until you confirm, and removed items wait in quarantine so you can bring
      them back.
    </p>
  </header>

  <ul class="drives">
    {#each ui.status?.drives ?? [] as d}
      {@const used = d.total - d.free}
      {@const pct = d.total ? (used / d.total) * 100 : 0}
      <li>
        <div class="name">
          <h2>{d.root}</h2>
          <span class="muted">
            {d.label || (d.removable ? 'Removable drive' : 'Local disk')}, {d.fileSystem}
          </span>
        </div>
        <div
          class="meter"
          role="meter"
          aria-valuenow={Math.round(pct)}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label="{d.root} used"
        >
          <span style:width="{pct}%" class:full={pct > 90}></span>
        </div>
        <span class="space">
          <strong>{bytes(d.free)} free</strong>
          <span class="muted">of {bytes(d.total)}</span>
        </span>
        <button class="primary" disabled={ui.scanning} onclick={() => startScan(d.root)}>
          Scan {d.root.slice(0, 2)}
        </button>
      </li>
    {/each}
  </ul>

  <div class="toolbar">
    <button disabled={ui.scanning} onclick={pickFolder}>Scan a folder…</button>
    {#if ui.status && !ui.status.elevated}
      <p class="muted">
        Running without admin rights: scans walk every folder, which takes longer, and some system
        folders cannot be read.
      </p>
    {:else if ui.status?.elevated}
      <p class="muted">Running as administrator: drive scans read the file table directly.</p>
    {/if}
  </div>
</div>

<style>
  .drives {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    max-width: 920px;
    border-top: 1px solid var(--line);
  }
  li {
    display: grid;
    grid-template-columns: 170px minmax(0, 1fr) 190px auto;
    align-items: center;
    gap: 20px;
    padding: 18px 0;
    border-bottom: 1px solid var(--line);
  }
  .name {
    display: flex;
    flex-direction: column;
  }
  .name h2 {
    font-size: 24px;
  }
  .meter {
    height: 14px;
    background: var(--sunken);
    border-radius: var(--radius-m);
    overflow: hidden;
  }
  .meter span {
    display: block;
    height: 100%;
    background: var(--ink);
    opacity: 0.75;
  }
  .meter span.full {
    background: var(--never);
    opacity: 1;
  }
  .space {
    white-space: nowrap;
  }
</style>
