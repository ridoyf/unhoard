<script lang="ts">
  import { Events } from '@wailsio/runtime'
  import { onDestroy } from 'svelte'
  import { api, type DupGroup } from '../lib/api'
  import { activities, endActivity, startActivity } from '../lib/activity.svelte'
  import { bytes, date, errorText, plural } from '../lib/format'
  import { askQuarantine, attempt, reveal, ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'
  import Choice from '../components/Choice.svelte'

  let minMB = $state(1)
  let groups = $state<DupGroup[] | null>(null)
  let error = $state('')
  let checked: string[] = $state([]) // paths
  // The search belongs to the app, not this page, so it keeps showing after leaving the page.
  const running = $derived(!!activities.duplicates)

  const off = Events.On('dups:done', (e) => (groups = e.data ?? []))
  onDestroy(off)

  // The backend sends null both before any search and once every set is resolved,
  // so null only resets the list after this page has shown results.
  $effect(() => {
    void ui.revision
    api.Duplicates().then((g) => {
      if (g?.length || groups) groups = g ?? []
    })
  })

  async function start() {
    error = ''
    startActivity('duplicates', 'Finding duplicates')
    try {
      await api.StartDuplicates(minMB)
      checked = []
    } catch (e) {
      endActivity('duplicates')
      error = errorText(e)
    }
  }

  function keepNewest() {
    checked = (groups ?? []).flatMap((g) => {
      const files = [...(g.files ?? [])].sort((a, b) => b.modified - a.modified)
      return files.slice(1).map((f) => f.path)
    })
  }

  function toggle(path: string) {
    checked = ticked.has(path) ? checked.filter((p) => p !== path) : [...checked, path]
  }

  const ticked = $derived(new Set(checked))
  const files = $derived((groups ?? []).flatMap((g) => g.files ?? []))
  const picked = $derived(files.filter((f) => ticked.has(f.path)))
  const wasted = $derived(
    (groups ?? []).reduce((s, g) => s + g.bytes * ((g.files?.length ?? 1) - 1), 0),
  )
  // A group with every copy ticked would lose the file entirely.
  const wipesAGroup = $derived(
    (groups ?? []).some((g) => (g.files ?? []).every((f) => ticked.has(f.path))),
  )
  const shown = $derived((groups ?? []).slice(0, 300))
</script>

<div class="view">
  <header>
    <h1>Duplicates</h1>
    <p class="muted">
      Matched by content, not name. Online-only files are skipped so nothing downloads. Keep one
      copy of each.
    </p>
  </header>
  <div class="toolbar">
    <Choice
      label="At least"
      bind:value={minMB}
      disabled={running}
      options={[
        [0, 'any size'],
        [1, '1 MB'],
        [10, '10 MB'],
        [100, '100 MB'],
      ]}
    />
    {#if running}
      <button onclick={() => attempt(api.CancelDuplicates())}>Cancel</button>
    {:else}
      <button class="primary" onclick={start}>{groups ? 'Search again' : 'Find duplicates'}</button>
    {/if}
  </div>
  {#if running}
    <Busy
      activity={activities.duplicates}
      hint="Large files take a while to read in full. The count moves once each one is compared."
    />
  {/if}
  {#if error}
    <p class="error-text" role="alert">{error}</p>
  {/if}

  {#if groups && !running}
    <p>
      <strong>{plural(groups.length, 'set')} of duplicates, {bytes(wasted)} in extra copies</strong>
    </p>
    {#if groups.length}
      <div class="toolbar">
        <button onclick={keepNewest}>Select all but the newest copy</button>
        <button onclick={() => (checked = [])}>Clear selection</button>
        <span class="muted">
          {picked.length} selected, {bytes(picked.reduce((s, f) => s + f.bytes, 0))}
        </span>
        <div class="grow"></div>
        <button
          class="primary"
          disabled={!picked.length || wipesAGroup}
          title={wipesAGroup ? 'Leave at least one copy of every file unticked' : ''}
          onclick={() => askQuarantine(picked)}
        >
          Move selected to quarantine…
        </button>
      </div>
    {/if}
    <div role="alert">
      {#if wipesAGroup}
        <p class="error-text">
          Every copy of at least one file is ticked. Untick one so a copy is kept.
        </p>
      {/if}
    </div>
    <ol class="groups">
      {#each shown as g, i (i)}
        <li>
          <div class="ghead">
            <strong>{bytes(g.bytes)}</strong>
            <span class="muted">each, {g.files?.length} copies</span>
          </div>
          {#each g.files ?? [] as f}
            <div class="file">
              <label>
                <input
                  type="checkbox"
                  checked={ticked.has(f.path)}
                  onchange={() => toggle(f.path)}
                />
                <span class="path">{f.path}</span>
              </label>
              <span class="muted">{date(f.modified)}</span>
              <button class="quiet" onclick={() => reveal(f.path)}>Go to folder</button>
            </div>
          {/each}
        </li>
      {/each}
    </ol>
    {#if groups.length > shown.length}
      <p class="muted">Showing the 300 sets that waste the most space.</p>
    {/if}
  {/if}
</div>

<style>
  .groups {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 1000px;
  }
  .groups li {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-m);
    padding: 8px 12px;
  }
  .ghead {
    padding-bottom: 4px;
  }
  .file {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    gap: 10px;
    align-items: center;
    padding: 2px 0;
  }
  .file label {
    display: flex;
    gap: 10px;
    align-items: center;
    min-width: 0;
  }
  .path {
    word-break: break-all;
  }
  .file button {
    min-height: 0;
    padding: 0 6px;
  }
</style>
