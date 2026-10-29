<script lang="ts">
  import { api, type Growth, type Snapshot } from '../lib/api'
  import { bytes, dateTime } from '../lib/format'
  import { loader } from '../lib/load.svelte'
  import { attempt, reveal, ui } from '../lib/state.svelte'
  import Busy from '../components/Busy.svelte'
  import LineChart from '../components/LineChart.svelte'

  let root = $state('')
  let since = $state(0)
  let folder = $state('')

  const res = loader(async () => (await api.Snapshots()) ?? [])
  const growth = loader(async () => (since && ui.scan?.root === root ? api.GrewSince(since) : null))
  const history = loader(async () =>
    root ? ((await api.FolderHistory(root, folder || root)) ?? []) : [],
  )
  const report = $derived(growth.data)
  const trend = $derived(history.data ?? [])
  const snaps = $derived(res.data ?? [])
  $effect(() => {
    if (!root) root = ui.scan?.root ?? snaps[0]?.root ?? ''
  })

  function chooseRoot() {
    since = 0
    folder = ''
  }

  const roots = $derived([...new Set(snaps.map((s) => s.root))])
  const ofRoot = $derived(snaps.filter((s) => s.root === root))
  // The newest snapshot is the loaded scan itself, so compare against older ones.
  const older = $derived(ui.scan?.root === root ? ofRoot.slice(1) : ofRoot)
  const isDrive = $derived(/^[A-Za-z]:\\$/.test(root))

  $effect(() => {
    if (!since && older.length) since = older[0].id
  })

  function remove(s: Snapshot) {
    attempt(api.DeleteSnapshot(s.id).then(() => ui.revision++))
  }
</script>

{#snippet changes(title: string, list: Growth[] | null)}
  <div>
    <h3>{title}</h3>
    <ul>
      {#each list ?? [] as g}
        <li>
          <button
            class="quiet path"
            onclick={() => (folder = g.path)}
            title="Show this folder's trend"
          >
            {g.path}
          </button>
          <span class="delta">{g.delta > 0 ? '+' : '−'}{bytes(Math.abs(g.delta))}</span>
          <span class="muted">{g.isNew ? 'new' : `${bytes(g.before)} → ${bytes(g.after)}`}</span>
          <button class="quiet" onclick={() => reveal(g.path)}>Go to folder</button>
        </li>
      {:else}
        <li class="muted">Nothing changed by much.</li>
      {/each}
    </ul>
  </div>
{/snippet}

<div class="view">
  <header>
    <h1>History</h1>
    <p class="muted">Each scan is saved as a snapshot.</p>
  </header>

  {#if res.error}
    <p class="error-text" role="alert">{res.error}</p>
  {:else if !res.data}
    <Busy label="Loading snapshots" />
  {:else if snaps.length === 0}
    <p class="muted">No snapshots yet. Each completed scan adds one.</p>
  {:else}
    <div class="toolbar">
      <label>
        Drive or folder <select bind:value={root} onchange={chooseRoot}>
          {#each roots as r}
            <option>{r}</option>
          {/each}
        </select>
      </label>
    </div>

    <section>
      <h2>{folder || (isDrive ? `Used space on ${root}` : `Size of ${root}`)}</h2>
      {#if folder}
        <button class="quiet back" onclick={() => (folder = '')}>Back to the whole drive</button>
      {/if}
      {#if history.error}
        <p class="error-text" role="alert">{history.error}</p>
      {:else if trend.length > 0}
        <LineChart points={trend} label={folder || root} />
      {:else}
        <p class="muted">Not enough snapshots to draw a trend.</p>
      {/if}
    </section>

    <section>
      <h2>What grew</h2>
      {#if ui.scan?.root !== root}
        <p class="muted">Scan {root} to compare it with earlier snapshots.</p>
      {:else if older.length === 0}
        <p class="muted">Scan again later to see what changed since today.</p>
      {:else}
        <label>
          Since <select bind:value={since}>
            {#each older as s}
              <option value={s.id}>{dateTime(s.at)}</option>
            {/each}
          </select>
        </label>
        {#if growth.error}
          <p class="error-text" role="alert">{growth.error}</p>
        {/if}
        {#if report}
          <div class="cols">
            {@render changes('Grew', report.grew)}
            {@render changes('Shrank', report.shrank)}
          </div>
        {/if}
      {/if}
    </section>

    <section>
      <h2>Snapshots</h2>
      <table class="data-table">
        <thead>
          <tr>
            <th>Taken</th>
            <th>Of</th>
            <th class="num">Found</th>
            <th class="num">Drive used</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#each ofRoot as s (s.id)}
            <tr>
              <td>{dateTime(s.at)}</td>
              <td>{s.root}</td>
              <td class="num">{bytes(s.bytes)}</td>
              <td class="num">{s.volTotal ? bytes(s.volTotal - s.volFree) : ''}</td>
              <td class="num"><button class="quiet" onclick={() => remove(s)}>Delete</button></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </section>
  {/if}
</div>

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 1000px;
    padding: 16px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: var(--radius-m);
  }
  .back {
    align-self: flex-start;
    padding-left: 0;
    color: var(--accent);
  }
  .cols {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 20px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto auto;
    gap: 8px;
    align-items: center;
    padding: 3px 0;
    border-bottom: 1px solid var(--line);
    font-size: var(--text-m);
  }
  .path {
    text-align: left;
    padding: 0;
    min-height: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .delta {
    font-weight: 600;
  }
  li button {
    min-height: 0;
    padding: 0 6px;
  }
</style>
